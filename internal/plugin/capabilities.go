package plugin

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dop251/goja"
)

const maxResponseBytes = 2 << 20

// All capability callbacks run on the VM worker. Timer goroutines only dispatch.
func (r *jsRuntime) installCapabilities(vm *goja.Runtime, ctx *goja.Object) {
	r.installFilesystem(vm, ctx)
	ui := vm.NewObject()
	ui.Set("list", func(call goja.FunctionCall) goja.Value { return call.Argument(0) })
	ctx.Set("ui", ui)
	if r.perms.Granted("background") {
		bg := vm.NewObject()
		bg.Set("every", func(call goja.FunctionCall) goja.Value {
			ms := call.Argument(0).ToInteger()
			fn, ok := goja.AssertFunction(call.Argument(1))
			if ms < 1000 || ms > 86400000 || !ok {
				panic(vm.NewGoError(Errorf(r.pluginID, ErrInvalidArgument, "every requires 1000..86400000ms and a callback")))
			}
			r.tasksMu.Lock()
			if len(r.tasks) >= 16 {
				r.tasksMu.Unlock()
				panic(vm.NewGoError(Errorf(r.pluginID, ErrInvalidArgument, "at most 16 background tasks")))
			}
			r.nextTask++
			id := r.nextTask
			stop := make(chan struct{})
			r.tasks[id] = stop
			r.tasksMu.Unlock()
			go r.runTimer(time.Duration(ms)*time.Millisecond, fn, stop)
			return vm.ToValue(id)
		})
		bg.Set("cancel", func(id int) {
			r.tasksMu.Lock()
			defer r.tasksMu.Unlock()
			if stop, ok := r.tasks[id]; ok {
				close(stop)
				delete(r.tasks, id)
			}
		})
		ctx.Set("background", bg)
	}
	for name := range r.perms.granted {
		if permissionBase(name) == "network" {
			network := vm.NewObject()
			network.Set("request", func(call goja.FunctionCall) goja.Value {
				options := call.Argument(1)
				method, body := "GET", ""
				followRedirects := true
				headers := map[string]string{}
				if !isAbsent(options) {
					obj := options.ToObject(vm)
					if v := obj.Get("followRedirects"); !isAbsent(v) {
						followRedirects = v.ToBoolean()
					}
					if v := obj.Get("method"); !isAbsent(v) {
						method = v.String()
					}
					if v := obj.Get("body"); !isAbsent(v) {
						body = v.String()
					}
					if v := obj.Get("headers"); !isAbsent(v) {
						h := v.ToObject(vm)
						for _, k := range h.Keys() {
							headers[k] = h.Get(k).String()
						}
					}
				}
				response, err := r.requestWithRedirects(call.Argument(0).String(), method, body, headers, followRedirects)
				if err != nil {
					panic(vm.NewGoError(err))
				}
				return vm.ToValue(response)
			})
			ctx.Set("network", network)
			break
		}
	}
}

func (r *jsRuntime) runTimer(interval time.Duration, fn goja.Callable, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-r.quit:
			return
		case <-ticker.C:
			if !r.backgroundEnabled.Load() {
				continue
			}
			_, err := r.call(context.Background(), ActionTimeout, func(vm *goja.Runtime, deadline time.Time) (any, error) {
				select {
				case <-stop:
					return nil, nil
				default:
				}
				if !r.backgroundEnabled.Load() {
					return nil, nil
				}
				value, err := fn(goja.Undefined())
				if err != nil {
					return nil, wrapJS(r.pluginID, err)
				}
				return r.await(vm, value, deadline)
			})
			if err != nil {
				log.Printf("plugin %s background: %v", r.pluginID, err)
			}
		}
	}
}

func (r *jsRuntime) stopTasks() {
	r.backgroundEnabled.Store(false)
	r.tasksMu.Lock()
	defer r.tasksMu.Unlock()
	for id, stop := range r.tasks {
		close(stop)
		delete(r.tasks, id)
	}
}

func (r *jsRuntime) checkURL(u *url.URL) error {
	if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || !r.perms.Granted("network:"+strings.ToLower(u.Hostname())) {
		return Errorf(r.pluginID, ErrNetworkBlocked, "URL scheme or hostname is not allowed: %s", u.Hostname())
	}
	return nil
}

func (r *jsRuntime) request(rawURL, method, body string, headers map[string]string) (map[string]any, error) {
	return r.requestWithRedirects(rawURL, method, body, headers, true)
}

func (r *jsRuntime) requestWithRedirects(rawURL, method, body string, headers map[string]string, followRedirects bool) (map[string]any, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, Errorf(r.pluginID, ErrInvalidArgument, "invalid URL")
	}
	if err := r.checkURL(u); err != nil {
		return nil, err
	}
	deadline := r.deadline
	if max := time.Now().Add(ActionTimeout); deadline.IsZero() || deadline.After(max) {
		deadline = max
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	// Shutdown cancels blocked HTTP independently of VM interrupts.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-r.quit:
			cancel()
		case <-done:
		}
	}()
	req, err := http.NewRequestWithContext(ctx, method, rawURL, strings.NewReader(body))
	if err != nil {
		return nil, Errorf(r.pluginID, ErrInvalidArgument, "invalid request")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !followRedirects {
			return http.ErrUseLastResponse
		}
		previous := via[len(via)-1].URL
		if previous.Scheme == "https" && req.URL.Scheme != "https" {
			return Errorf(r.pluginID, ErrNetworkBlocked, "HTTPS redirect downgrade blocked")
		}
		original := via[0].URL
		if !strings.EqualFold(original.Scheme, req.URL.Scheme) || !strings.EqualFold(original.Host, req.URL.Host) {
			// Go strips some standard credentials, but custom API-key headers need protection too.
			for name := range headers {
				req.Header.Del(name)
			}
			req.Header.Del("Authorization")
			req.Header.Del("Proxy-Authorization")
			req.Header.Del("Cookie")
			req.Header.Del("Referer")
		}
		if len(via) >= 10 {
			return Errorf(r.pluginID, ErrNetworkBlocked, "too many redirects")
		}
		return r.checkURL(req.URL)
	}}
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, Errorf(r.pluginID, ErrTimeout, "network request timed out or cancelled")
		}
		if code, ok := CodeOf(err); ok {
			return nil, Errorf(r.pluginID, code, "request blocked")
		}
		return nil, Errorf(r.pluginID, ErrHostAPIError, "network request failed")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return nil, Errorf(r.pluginID, ErrHostAPIError, "response read failed")
	}
	if len(data) > maxResponseBytes {
		return nil, Errorf(r.pluginID, ErrHostAPIError, "response exceeds 2MiB")
	}
	responseHeaders := map[string]string{}
	for k, v := range res.Header {
		responseHeaders[strings.ToLower(k)] = strings.Join(v, ", ")
	}
	return map[string]any{"status": res.StatusCode, "ok": res.StatusCode >= 200 && res.StatusCode < 300, "headers": responseHeaders, "body": string(data)}, nil
}

package plugin

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"

	"kyvro/internal/core"
)

const (
	// activateTimeout bounds the optional activate() hook (spec §15 targets
	// 100ms; the bound is generous headroom for storage warm-up).
	activateTimeout = 2 * time.Second
	// promisePollInterval paces polling of async activate/search results.
	promisePollInterval = 2 * time.Millisecond
)

// modulePrelude provides the CommonJS module protocol. goja has no ESM
// support, so plugins export via module.exports (documented deviation from
// the spec's `export const` examples, to be fixed by the M2 toolchain).
const modulePrelude = "var module = { exports: {} };\nvar exports = module.exports;\n"

// runtime owns one goja VM and a worker goroutine that is the only
// goroutine ever touching the VM. All JS execution (search, actions) is
// dispatched through a channel with a deadline; expiry interrupts the VM,
// the late result is dropped, and the host never crashes on plugin
// exceptions or runtime panics.
type jsRuntime struct {
	pluginID string
	vm       *goja.Runtime
	storage  *PluginStorage // non-nil only with a granted storage permission
	iconPath string         // absolute path of the manifest icon ("" when none)

	searchFn goja.Callable // exports.provider.search (nil when absent)
	actionFn goja.Callable // exports.onAction (nil when absent)
	hasProv  bool

	reqs     chan runtimeReq
	quit     chan struct{}
	done     chan struct{}
	quitMu   sync.Mutex // serializes ClearInterrupt against shutdown interrupts
	stopOnce sync.Once

	consecutiveTimeouts atomic.Int32
}

type runtimeReq struct {
	fn       func(vm *goja.Runtime, deadline time.Time) (any, error)
	deadline time.Time
	res      chan runtimeRes
}

type runtimeRes struct {
	val any
	err error
}

// newRuntime loads main, evaluates the module, runs the optional activate
// hook and starts the worker goroutine. dir is the resolved version
// directory.
func newRuntime(m *Manifest, dir string, storage *PluginStorage) (*jsRuntime, error) {
	src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(m.Main)))
	if err != nil {
		return nil, Errorf(m.ID, ErrInvalidArgument, "read main %s: %v", m.Main, err)
	}
	vm := goja.New()
	r := &jsRuntime{
		pluginID: m.ID,
		vm:       vm,
		storage:  storage,
		reqs:     make(chan runtimeReq),
		quit:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	if m.Icon != "" {
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(m.Icon))); err == nil && !info.IsDir() {
			r.iconPath = filepath.Join(dir, filepath.FromSlash(m.Icon))
		}
	}

	prog, err := goja.Compile(m.Main, modulePrelude+string(src), false)
	if err != nil {
		return nil, Errorf(m.ID, ErrPluginException, "compile %s: %v", m.Main, err)
	}
	if _, err := vm.RunProgram(prog); err != nil {
		return nil, wrapJS(m.ID, err)
	}

	mod := vm.Get("module")
	if mod == nil {
		return nil, Errorf(m.ID, ErrPluginException, "module prelude missing")
	}
	exportsVal := mod.ToObject(vm).Get("exports")
	if isAbsent(exportsVal) {
		return nil, Errorf(m.ID, ErrPluginException, "module.exports is undefined")
	}
	exports := exportsVal.ToObject(vm)

	if provider := exports.Get("provider"); !isAbsent(provider) {
		if search := provider.ToObject(vm).Get("search"); search != nil {
			if fn, ok := goja.AssertFunction(search); ok {
				r.searchFn = fn
				r.hasProv = true
			}
		}
	}
	if onAction := exports.Get("onAction"); !isAbsent(onAction) {
		if fn, ok := goja.AssertFunction(onAction); ok {
			r.actionFn = fn
		}
	}

	if activate := exports.Get("activate"); !isAbsent(activate) {
		fn, ok := goja.AssertFunction(activate)
		if !ok {
			return nil, Errorf(m.ID, ErrInvalidArgument, "activate is not a function")
		}
		ctxObj := r.buildContext(vm)
		if err := runBounded(vm, activateTimeout, func(deadline time.Time) error {
			res, err := fn(goja.Undefined(), ctxObj)
			if err != nil {
				return err
			}
			_, err = r.await(vm, res, deadline)
			return err
		}); err != nil {
			return nil, wrapJS(m.ID, err)
		}
	}

	go r.work()
	return r, nil
}

// buildContext assembles the JS-visible PluginContext (V1: storage when
// granted, plus logging). Unimplemented capability APIs are simply absent.
func (r *jsRuntime) buildContext(vm *goja.Runtime) goja.Value {
	ctx := vm.NewObject()
	if r.storage != nil {
		st := vm.NewObject()
		st.Set("get", func(call goja.FunctionCall) goja.Value {
			v, ok, err := r.storage.Get(call.Argument(0).String())
			if err != nil {
				panic(vm.NewGoError(err))
			}
			if !ok {
				return nil // undefined
			}
			return vm.ToValue(v)
		})
		st.Set("set", func(call goja.FunctionCall) goja.Value {
			key := call.Argument(0).String()
			value := call.Argument(1).String()
			if err := r.storage.Set(key, value); err != nil {
				panic(vm.NewGoError(err))
			}
			return nil
		})
		st.Set("delete", func(call goja.FunctionCall) goja.Value {
			if err := r.storage.Delete(call.Argument(0).String()); err != nil {
				panic(vm.NewGoError(err))
			}
			return nil
		})
		ctx.Set("storage", st)
	}
	lg := vm.NewObject()
	for _, level := range []string{"info", "warn", "error"} {
		lg.Set(level, func(call goja.FunctionCall) goja.Value {
			parts := make([]string, 0, len(call.Arguments))
			for _, a := range call.Arguments {
				parts = append(parts, a.String())
			}
			log.Printf("plugin %s [%s]: %s", r.pluginID, level, strings.Join(parts, " "))
			return nil
		})
	}
	ctx.Set("log", lg)

	// Add template function registration support
	// Plugins can register functions for use in Text Snippets
	// Usage: ctx.template.registerFunc("upper", (args) => args[0].toUpperCase())
	tpl := vm.NewObject()
	tpl.Set("registerFunc", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewGoError(errors.New("registerFunc requires 2 arguments: name and function")))
		}
		name := call.Argument(0).String()
		fnVal := call.Argument(1)

		// Verify it's a function
		fnObj, ok := goja.AssertFunction(fnVal)
		if !ok {
			panic(vm.NewGoError(errors.New("second argument must be a function")))
		}

		// Capture the vm for later calls
		fn := func(args ...string) (string, error) {
			vmArgs := make([]goja.Value, len(args))
			for i, arg := range args {
				vmArgs[i] = vm.ToValue(arg)
			}
			result, err := fnObj(goja.Undefined(), vmArgs...)
			if err != nil {
				return "", wrapJS(r.pluginID, err)
			}
			if result == nil || goja.IsUndefined(result) || goja.IsNull(result) {
				return "", nil
			}
			return result.ToString().String(), nil
		}
		core.RegisterTemplateFunc(name, fn)
		return nil
	})
	ctx.Set("template", tpl)

	return ctx
}

// runBounded executes f (on a helper goroutine) under a wall-clock timeout.
// On expiry the VM is interrupted, which makes f return promptly; the result
// is normalized to a TIMEOUT PluginError.
func runBounded(vm *goja.Runtime, timeout time.Duration, f func(deadline time.Time) error) error {
	deadline := time.Now().Add(timeout)
	res := make(chan error, 1)
	go func() {
		defer func() {
			if x := recover(); x != nil {
				res <- Errorf("", ErrPluginException, "panic: %v", x)
			}
		}()
		res <- f(deadline)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-res:
		return err
	case <-timer.C:
		vm.Interrupt("timeout")
		return Errorf("", ErrTimeout, "timed out after %s", timeout)
	}
}

// call dispatches fn to the worker and waits for the result with a timeout.
// On timeout the VM is interrupted, the (buffered) late result is dropped,
// and TIMEOUT is returned. Three consecutive timeouts disable the plugin
// (checked by the manager via Strikes).
func (r *jsRuntime) call(ctx context.Context, timeout time.Duration, fn func(vm *goja.Runtime, deadline time.Time) (any, error)) (any, error) {
	if timeout <= 0 {
		timeout = 150 * time.Millisecond
	}
	req := runtimeReq{
		fn:       fn,
		deadline: time.Now().Add(timeout),
		res:      make(chan runtimeRes, 1), // buffered: the worker never blocks on dropped results
	}
	select {
	case r.reqs <- req:
	case <-r.quit:
		return nil, Errorf(r.pluginID, ErrPluginException, "runtime is shutting down")
	case <-ctx.Done():
		return nil, Errorf(r.pluginID, ErrTimeout, "cancelled before dispatch: %v", ctx.Err())
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-req.res:
		r.consecutiveTimeouts.Store(0)
		return res.val, res.err
	case <-timer.C:
		r.vm.Interrupt("timeout")
		r.consecutiveTimeouts.Add(1)
		return nil, Errorf(r.pluginID, ErrTimeout, "timed out after %s", timeout)
	case <-r.quit:
		return nil, Errorf(r.pluginID, ErrPluginException, "runtime is shutting down")
	case <-ctx.Done():
		r.vm.Interrupt("cancelled")
		return nil, Errorf(r.pluginID, ErrTimeout, "cancelled: %v", ctx.Err())
	}
}

// work is the only goroutine that touches the VM once it starts.
func (r *jsRuntime) work() {
	defer close(r.done)
	for {
		select {
		case <-r.quit:
			return
		case req := <-r.reqs:
			if !r.prepareVM() {
				req.res <- runtimeRes{err: Errorf(r.pluginID, ErrPluginException, "runtime is shutting down")}
				return
			}
			val, err := r.safeRun(req)
			req.res <- runtimeRes{val: val, err: err}
		}
	}
}

// prepareVM clears a stale interrupt left by a previous timed-out call. The
// quitMu-held check is what makes Shutdown race-free: once quit is closed
// (under the same mutex), no ClearInterrupt can erase the shutdown
// interrupt.
func (r *jsRuntime) prepareVM() bool {
	r.quitMu.Lock()
	defer r.quitMu.Unlock()
	select {
	case <-r.quit:
		return false
	default:
		r.vm.ClearInterrupt()
		return true
	}
}

// safeRun converts panics from fn into PLUGIN_EXCEPTION so the host never
// crashes.
func (r *jsRuntime) safeRun(req runtimeReq) (val any, err error) {
	defer func() {
		if x := recover(); x != nil {
			val, err = nil, Errorf(r.pluginID, ErrPluginException, "runtime panic: %v", x)
		}
	}()
	return req.fn(r.vm, req.deadline)
}

// Shutdown stops the worker and waits for it to exit. Safe to call once;
// later calls just wait on done.
func (r *jsRuntime) Shutdown() {
	r.stopOnce.Do(func() {
		r.quitMu.Lock()
		close(r.quit)
		r.vm.Interrupt("shutdown") // break any in-flight JS loop
		r.quitMu.Unlock()
	})
	<-r.done
}

// Strikes returns the number of consecutive timed-out calls.
func (r *jsRuntime) Strikes() int { return int(r.consecutiveTimeouts.Load()) }

// resetStrikes clears the consecutive-timeout counter (on re-enable).
func (r *jsRuntime) resetStrikes() { r.consecutiveTimeouts.Store(0) }

// HasProvider reports whether the module exports a usable provider.search.
func (r *jsRuntime) HasProvider() bool { return r.hasProv }

// Search runs provider.search(query) on the worker VM.
func (r *jsRuntime) Search(ctx context.Context, query string, timeout time.Duration) ([]core.SearchResult, error) {
	if r.searchFn == nil {
		return nil, nil
	}
	v, err := r.call(ctx, timeout, func(vm *goja.Runtime, deadline time.Time) (any, error) {
		res, err := r.searchFn(goja.Undefined(), vm.ToValue(query))
		if err != nil {
			return nil, wrapJS(r.pluginID, err)
		}
		return r.await(vm, res, deadline)
	})
	if err != nil {
		return nil, err
	}
	return r.convertLogged(v)
}

// RunAction runs onAction(actionID, args...) on the worker VM.
func (r *jsRuntime) RunAction(ctx context.Context, actionID string, args []string, timeout time.Duration) ([]core.SearchResult, error) {
	if r.actionFn == nil {
		return nil, Errorf(r.pluginID, ErrInvalidArgument, "module does not export onAction")
	}
	v, err := r.call(ctx, timeout, func(vm *goja.Runtime, deadline time.Time) (any, error) {
		// SDK shape: onAction(actionId, args[]) — args as ONE array value.
		res, err := r.actionFn(goja.Undefined(), vm.ToValue(actionID), vm.ToValue(args))
		if err != nil {
			return nil, wrapJS(r.pluginID, err)
		}
		return r.await(vm, res, deadline)
	})
	if err != nil {
		return nil, err
	}
	return r.convertLogged(v)
}

// convertLogged converts an exported JS value and logs dropped entries.
func (r *jsRuntime) convertLogged(v any) ([]core.SearchResult, error) {
	results, dropped, err := convertResults(r.pluginID, r.iconPath, v)
	if dropped > 0 {
		log.Printf("plugin %s: dropped %d invalid result(s)", r.pluginID, dropped)
	}
	return results, err
}

// await resolves a possibly-Promise return value before exporting it. The
// poll deadline keeps never-settling promises from hanging the worker.
func (r *jsRuntime) await(vm *goja.Runtime, v goja.Value, deadline time.Time) (any, error) {
	p, ok := v.Export().(*goja.Promise)
	if !ok {
		return v.Export(), nil
	}
	for p.State() == goja.PromiseStatePending {
		if time.Now().After(deadline) {
			return nil, Errorf(r.pluginID, ErrTimeout, "promise did not settle in time")
		}
		// Give queued microtasks a chance to run (goja drains jobs when
		// the call stack empties, i.e. on script boundaries).
		_, _ = vm.RunScript("kyvro:tick", "undefined")
		time.Sleep(promisePollInterval)
	}
	if p.State() == goja.PromiseStateRejected {
		return nil, Errorf(r.pluginID, ErrPluginException, "promise rejected: %v", p.Result())
	}
	return p.Result().Export(), nil
}

// wrapJS converts goja errors (JS exceptions, interrupts, compile errors)
// into PluginErrors.
func wrapJS(pluginID string, err error) error {
	if err == nil {
		return nil
	}
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		return Errorf(pluginID, ErrTimeout, "interrupted: %v", ie.Value())
	}
	return Errorf(pluginID, ErrPluginException, "%v", err)
}

func isAbsent(v goja.Value) bool {
	return v == nil || v.Export() == nil // undefined or null
}

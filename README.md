# Kyvro

A cross-platform, keyboard-first launcher (an open Alfred/Raycast). See `docs/spec.md` for the full vision.

**v0.1 scope (macOS first):** summon with ⌥Space, fuzzy app search, frecency ranking, `Search Google for "…"` fallback, keyboard navigation, and persistent usage history (bbolt). Clipboard, plugins/workflows, file indexing and Windows/Linux come later — the layering below leaves room for them.

## Run

Prerequisites: Go ≥ 1.25, Node + pnpm, macOS (v0.1). A `wails3` CLI (v3.0.0-beta.12) is only needed for `dev` mode and regenerating bindings.

```sh
pnpm --dir frontend install
pnpm --dir frontend build
go build -o bin/kyvro .
./bin/kyvro
```

Then press ⌥Space (or ⌥⌘Space if ⌥Space is taken — check the log). The app lives in the menu bar ("Kyvro" → Show / Quit), hides on focus loss, and refuses to start twice.

For hot-reload development: `wails3 dev`.

## Architecture

Per the spec, the core stays independent of Wails and the UI; only `main.go` and `service/` know about the framework.

```
main.go                  launcher-app: window, ⌥Space hotkey, systray, single instance
service/                 Wails-bound thin bridge (Search/Launch)
internal/core/           pure Go: model, Provider iface, engine, frecency rank, bbolt store
internal/providers/      apps (fuzzy over platform.AppSource) + web fallback
internal/platform/       AppSource/AppLauncher interfaces; darwin impl + !darwin stubs
frontend/                Vue 3 + TS + Vite + Tailwind, bindings generated into frontend/bindings
```

- **Ranking:** `score = fuzzy + 8·log₂(count+1) + 12·2^(−age/72h)` — relevance first, usage breaks ties (tests in `internal/core`).
- **Data:** usage history in `~/Library/Application Support/Kyvro/data.db` (bbolt).
- **App scan:** `/Applications`, `/System/Applications`, `~/Applications` (incl. one nesting level, e.g. Utilities) parsed via `Info.plist`; full scan at startup, then throttled to 1/min in the background.

## Tests

```sh
go vet ./...
go test ./internal/...
```

## Regenerate bindings

After changing bound service methods:

```sh
wails3 generate bindings -d frontend/bindings -clean -names
```

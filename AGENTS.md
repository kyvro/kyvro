# Repository Guidelines

## Project Structure

- main.go: Wails entry — window, ⌥Space hotkey, systray, single instance
- service/: Wails-bound SearchService (Search/Launch), thin bridge only
- internal/core/: pure Go launcher core (model, Provider, engine, rank, bbolt store)
- internal/providers/: apps (fuzzy over platform.AppSource), web (Google fallback)
- internal/platform/: AppSource/AppLauncher interfaces; darwin/ impl; unsupported stub (!darwin)
- frontend/: Vue 3 + TS + Tailwind v4; generated bindings in frontend/bindings/
- build/ + Taskfile.yml: wails3 task system (root Taskfile dispatches to build/<os>/)

## Commands

```bash
wails3 dev                          # hot-reload dev (vite port 9245)
wails3 build                        # frontend + go binary -> bin/kyvro
go vet ./... && go test ./internal/...
go build -o bin/kyvro .              # requires frontend/dist (pnpm --dir frontend build)
wails3 generate bindings -d frontend/bindings -clean -names
```

## Style

- internal/core must not import wails; platform access only via internal/platform interfaces
- gofmt required; keep service/ methods thin, business logic in core
- Wails pinned at v3.0.0-beta.12: verify API against module cache source (pkg/application/*.go), not website docs
- Do not downgrade wails/plist versions; go.mod declares go 1.25.0

## Testing

- All behavior changes in core/engine/rank/store/providers get unit tests
- Engine tests use injectable clock (Engine.SetNow) for deterministic frecency
- Fake platform sources in tests; no real app scans / GUI in tests
- Cross-compile check for platform layer: GOOS=linux go build ./... (embed errors about frontend/dist are expected pre-build)

## Agent Notes

- Use beta.12 CLI at ~/Go.proj/bin/wails3
- After changing bound service method signatures, regenerate bindings AND rebuild frontend
- Root Taskfile.yml is the dispatch taskfile (build -> {{.GOOS}}:build); never overwrite it with build/Taskfile.yml content
- Keep bbolt open inside ServiceStartup (after single-instance guard), or duplicate processes hit the db lock
- docs/spec.md = vision, docs/features.md = implemented state; keep features.md current

## Domain Knowledge

- Rank: score = fuzzy + 8·log₂(count+1) + 12·2^(−age/72h); fuzzy relevance dominates
- Provider order is priority order; web provider must stay last (tail fallback)
- Empty query returns full app list (score 0); engine then orders by frecency, tie-break by title
- App scan roots: /Applications, /System/Applications, ~/Applications, depth ≤ 2; skip LSUIItem agents
- Usage db: bbolt at ~/Library/Application Support/Kyvro/data.db (os.UserConfigDir)
- Hotkey: Alt+Space, fallback Alt+Cmd+Space; window hides on focus loss (HideOnFocusLost)

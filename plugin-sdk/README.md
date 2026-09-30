# @kyvro/plugin-sdk

Type declarations for Kyvro launcher plugins.

The package is **types-only**: it provides `index.d.ts` so plugin authors get IDE autocomplete, hover documentation and static validation while writing plain JavaScript plugins. The Kyvro host (embedded goja VM) executes plugins; this package never runs at runtime. Declarations mirror the host implementation (`internal/plugin/*.go`) field by field.

## Installation

Add as a dev dependency in your plugin's local tooling (adjust `file:` or use `npm link` until published):

```bash
npm install -D @kyvro/plugin-sdk
```

Plugin code must not require the SDK at runtime — annotate with JSDoc only.

## Authoring Model

A plugin is an installed directory:

```text
~/Library/Application Support/Kyvro/plugins/
└── com.example.demo/          # id must match the directory name
    ├── plugin.json            # manifest, schemaVersion 1
    └── 1.0.0/                 # SemVer version directories
        ├── plugin.json        # (resolved copy of the manifest)
        ├── index.js           # entry ("main")
        └── icon.svg
```

`plugin.json`:

```json
{
  "schemaVersion": 1,
  "id": "com.example.demo",
  "version": "1.0.0",
  "main": "index.js",
  "icon": "icon.svg",
  "minHostVersion": "0.1.0",
  "permissions": ["storage"],
  "commands": [{ "id": "hello", "title": "Hello", "prefix": "demo" }]
}
```

`index.js` stays plain CommonJS with one JSDoc annotation:

```js
// @ts-check

/**
 * @type {import("@kyvro/plugin-sdk").Plugin}
 */
module.exports = {
  onAction(actionId, args) {
    // Live prefix hit: the FULL query including the prefix arrives as
    // args[0] ("demo vim" stays "demo vim") — slice it yourself.
    const query = String(args[0] ?? "");
    const term = query.replace(/^\S+\s*/, "");
    return term
      ? [{ id: "copy", title: term, actions: [{ type: "copy", value: term }] }]
      : [];
  },

  activate(ctx) {
    if (ctx.storage) {
      ctx.storage.set("runs", String(Number(ctx.storage.get("runs") ?? "0") + 1));
    }
    ctx.log.info("activated");
  }
};
```

## Exports Protocol

All members of `module.exports` are optional:

| Member | Purpose | Notes |
|---|---|---|
| `onAction(actionId, args[])` | Only business entry | **prefix hit (live)**: called on every keystroke while the query matches a declared command `prefix` (word-boundary match, case-insensitive); actionId is the command's `id`, the FULL query arrives as `args[0]`, ~150ms budget, three consecutive timeouts auto-disable the plugin. **callback action**: called once when the user runs a `callback` action; actionId is the action's `id`, `args` is the action's `args ?? []`, ~5s budget. |
| `activate(ctx)` | Init hook | ~2s budget incl. awaited Promise; warm up storage. The only lifecycle callback — no unload/deactivate hook exists (cleanup is host-side). |

There is no `require()` / ESM support inside the VM; a plugin is a single CommonJS script.

## Result Rows & Actions

`onAction` returns (or resolves to) arrays of rows:

```js
{
  id: "repo",                    // becomes plugin:<pluginId>:<rowId>
  title: "GitHub",
  subtitle: "Open github.com",
  scoreHint: 10,                 // clamped to 0..50
  actions: [                     // >= 1 action required
    { type: "open-url", url: "https://github.com" },
    { type: "copy", value: "text" },
    { type: "callback", id: "open-issues", args: ["a", "b"] }
  ]
}
```

The first action runs on Enter. Rows missing `id`, `title` or valid `actions` are silently dropped by the host (logged, never crash).

## PluginContext (V1)

Unimplemented capabilities are simply absent from the context — feature-detect (`if (ctx.storage)`):

| Capability | Availability | Surface |
|---|---|---|
| `storage` | Only when `"storage"` permission granted | Persistent string→string KV (`get`/`set`/`delete`), bucket `plugin:<id>` |
| `log` | Always | `info` / `warn` / `error(...parts)` into the app log |

## Permissions

Manifest permissions are `<capability>` or `<capability>:<scope>`. V1 grants `"storage"` only; other parsed capabilities (`network`, `filesystem`, `shell`, `clipboard`, `secrets`, `system`, `background`) never grant and their APIs do not exist on the context yet.

## Type Checking Without TypeScript

Recommended `jsconfig.json` for a plugin project:

```json
{
  "compilerOptions": {
    "checkJs": true,
    "strict": true,
    "module": "commonjs",
    "target": "ES2020"
  },
  "include": ["**/*.js"]
}
```

Or add `// @ts-check` at the top of individual files.

## Versioning

Two separate concepts:

- **SDK package version** (`@kyvro/plugin-sdk@x.y.z`) — SemVer over the declaration surface.
- **Host compatibility** — declared per plugin via `minHostVersion` in `plugin.json` (must not exceed the running host, `0.1.0` today); manifest schema is pinned at `schemaVersion: 1`.

## Development

```bash
npm install
npm test        # tsc --noEmit over index.d.ts and type tests
```

See `CHANGELOG.md` for release history.

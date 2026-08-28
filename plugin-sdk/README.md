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
  "activationEvents": ["onStartup", "onSearchPrefix:demo"],
  "permissions": ["storage"],
  "commands": [{ "id": "hello", "title": "Hello" }]
}
```

`index.js` stays plain CommonJS with one JSDoc annotation:

```js
// @ts-check

/**
 * @type {import("@kyvro/plugin-sdk").Plugin}
 */
module.exports = {
  provider: {
    search(query) {
      const term = query.replace(/^\S+\s*/, "");
      return term
        ? [{ id: "copy", title: term, actions: [{ type: "copy", value: term }] }]
        : [];
    }
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
| `provider.search(query)` | Live search | Runs only while the query matches a declared `onSearchPrefix:` event; receives the FULL query including the prefix. ~150ms budget per call; three consecutive timeouts auto-disable the plugin. |
| `onAction(actionId, args[])` | Callback entry | Invoked for `callback` actions and activated manifest commands (commands forward the whole query as `args[0]`). Must return rows. |
| `activate(ctx)` | Init hook | ~2s budget incl. awaited Promise; register template functions, warm up storage. |

There is no `require()` / ESM support inside the VM; a plugin is a single CommonJS script.

## Result Rows & Actions

Both `search` and `onAction` return (or resolve to) arrays of rows:

```js
{
  id: "repo",                    // becomes plugin:<pluginId>:<rowId>
  title: "GitHub",
  subtitle: "Open github.com",
  scoreHint: 10,                 // clamped to 0..50; fuzzy match still dominates
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
| `template` | Always | `registerFunc(name, fn)` for `${name(...)}` snippet templates |

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

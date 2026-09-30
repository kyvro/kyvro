import type {
  ManifestCommand,
  Permission,
  Platform,
  Plugin,
  PluginAction,
  PluginContext,
  PluginManifest,
  PluginStorage,
  ResultRow
} from "../index";

const manifest: PluginManifest = {
  schemaVersion: 1,
  id: "com.example.demo",
  version: "1.0.0",
  main: "index.js",
  minHostVersion: "0.1.0",
  platforms: ["darwin"],
  permissions: ["storage"],
  commands: [{ id: "say-hi", title: "Say Hi", prefix: "demo" }]
};

// Prefix is required on commands; title optional (defaults to id).
const commands: ManifestCommand[] = [
  { id: "say-hi", title: "Say Hi", prefix: "demo" },
  { id: "ping", prefix: "PING" } // matched case-insensitively
];

const permissions: Permission[] = ["storage", "network:request"];
const goosList: Platform[] = ["darwin"];

// Non-empty tuple requirement mirrors convert.go: rows need >=1 action and
// the FIRST one becomes primary.
const actions: [PluginAction, ...PluginAction[]] = [
  { type: "open-url", url: "https://github.com" },
  { type: "copy", value: "payload" }
];

const row: ResultRow = {
  id: "repo",
  title: "GitHub",
  subtitle: "Open github.com",
  scoreHint: 10,
  actions
};

const plugin: Plugin = {
  // Single business entry. Live prefix hits arrive on every keystroke with
  // the FULL query (prefix included) as args[0]; callback actions arrive
  // once with the action's own args.
  onAction(actionId, args) {
    return [
      {
        id: actionId,
        title: String(args[0] ?? actionId),
        actions: [{ type: "callback", id: "root", args: [] }]
      }
    ];
  },

  activate(ctx) {
    // storage exists only when granted — feature-detect.
    if (ctx.storage) {
      ctx.storage.set("runs", "1");
    }
    ctx.log.info("activated", manifest.version);
  }
};

// Storage surface is string -> string.
function roundtrip(s: PluginStorage): string | undefined {
  s.set("k", "v");
  return s.get("k");
}

// Missing capabilities are optional on the context.
function featureDetect(ctx: PluginContext): void {
  if (!ctx.storage) return;
  ctx.storage.delete("k");
}

export {
  manifest,
  commands,
  permissions,
  goosList,
  plugin,
  roundtrip,
  featureDetect
};

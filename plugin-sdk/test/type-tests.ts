import type {
  ActivationEvent,
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
  activationEvents: ["onStartup", "onSearchPrefix:demo", "onCommand:say-hi"],
  permissions: ["storage"],
  commands: [{ id: "say-hi", title: "Say Hi", keywords: ["hello"] }]
};

// Template-literal union assignability.
const events: ActivationEvent[] = [
  "onStartup",
  "onSearchPrefix:demo",
  "onCommand:say-hi"
];
const permissions: Permission[] = ["storage", "network:request"];
const goosList: Platform[] = ["darwin"];

// Commands surfaced without JS; title optional (defaults to id).
const commands: ManifestCommand[] = [
  { id: "say-hi", title: "Say Hi", subtitle: "Greets", keywords: ["hello"] },
  { id: "ping" }
];

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
  provider: {
    // Full query including the trigger prefix arrives as-is.
    search(query) {
      return query.length > 0 ? [row] : [];
    }
  },

  // Callback actions re-enter here; activated commands forward the whole
  // query as args[0].
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
    ctx.template.registerFunc(
      "upper",
      (...parts) => parts.join("").toUpperCase()
    );
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
  events,
  permissions,
  goosList,
  plugin,
  roundtrip,
  featureDetect
};

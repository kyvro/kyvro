// Infrastructure demo; this does not query model usage or credentials.
let pluginContext;
let pageIndex = 0;
const pages = ["Background", "Network", "Storage"];
function refresh() {
  pluginContext.storage.set("updated", new Date().toISOString());
  try {
    const response = pluginContext.network.request("https://api.github.com/zen", {
      headers: { "Accept": "text/plain", "User-Agent": "Kyvro-Plugin-Demo" }
    });
    pluginContext.storage.set("network", response.ok ? response.body : "HTTP " + response.status);
  } catch (error) {
    pluginContext.storage.set("network", String(error));
    pluginContext.log.warn("refresh failed", error);
  }
}
module.exports.activate = function (context) {
  pluginContext = context;
  pluginContext.background.every(60000, refresh);
};
module.exports.onAction = function (id) {
  if (id !== "demo") return [];
  return [{
    id: "entry", title: "Plugin Capabilities Demo", subtitle: "Enter to open the plugin detail page",
    actions: [{ type: "open-view", id: "capabilities", args: [] }]
  }];
};
module.exports.renderView = function (viewId) {
  if (viewId !== "capabilities") throw new Error("Unknown view");
  const values = [
    pluginContext.storage.get("updated") || "Waiting for first 60s timer",
    pluginContext.storage.get("network") || "No response cached; click Refresh now",
    "Per-plugin persistent cache; writes do not update the search index"
  ];
  return {
    title: "Capabilities · " + (pageIndex + 1) + "/" + pages.length,
    shortcuts: { left: "previous", right: "next", enter: "refresh" },
    root: {
      type: "column", gap: "large", children: [
        { type: "card", title: pages[pageIndex], children: [
          { type: "text", text: values[pageIndex] },
          { type: "text", tone: "muted", text: "Background runs every 60s. Left/right switches pages." }
        ] },
        { type: "card", title: "Example progress component", children: [
          { type: "progress", title: "Demo metric (static example, not model usage)", value: 35, max: 100 }
        ] },
        { type: "row", gap: "small", children: [
          { type: "button", text: "Previous", event: "previous" },
          { type: "button", text: "Refresh now", event: "refresh" },
          { type: "button", text: "Next", event: "next" }
        ] }
      ]
    }
  };
};
module.exports.onViewEvent = function (viewId, eventId) {
  if (viewId !== "capabilities") throw new Error("Unknown view");
  if (eventId === "previous") pageIndex = (pageIndex + pages.length - 1) % pages.length;
  else if (eventId === "next") pageIndex = (pageIndex + 1) % pages.length;
  else if (eventId === "refresh") refresh();
};

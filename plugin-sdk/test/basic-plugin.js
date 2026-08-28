// @ts-check

/**
 * JavaScript + JSDoc is the primary Kyvro plugin development mode: plain
 * CommonJS executed by the embedded goja VM, no compilation step, full
 * IntelliSense from index.d.ts.
 *
 * @type {import("../index").Plugin}
 */
module.exports = {
  provider: {
    /**
     * Live search; invoked only while the query starts with a prefix
     * declared in activationEvents ("onSearchPrefix:..."). Receives the
     * FULL query including the prefix. Keep fast (~150ms budget).
     *
     * @param {string} query - full query including the trigger prefix
     */
    search(query) {
      const term = query.replace(/^\S+\s*/, "");
      return term
        ? [{
            id: "copy-term",
            title: "Copy " + term,
            actions: [{ type: "copy", value: term }]
          }]
        : [];
    }
  },

  /**
   * Optional init hook (~2s budget). Typical use: storage warm-up,
   * template registrations under "onStartup".
   *
   * @param {import("../index").PluginContext} ctx
   */
  activate(ctx) {
    if (ctx.storage) {
      const runs = Number(ctx.storage.get("runs") ?? "0") + 1;
      ctx.storage.set("runs", String(runs));
    }
    ctx.log.info("basic-plugin activated");
  }
};

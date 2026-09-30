// @ts-check

/**
 * JavaScript + JSDoc is the primary Kyvro plugin development mode: plain
 * CommonJS executed by the embedded goja VM, no compilation step, full
 * IntelliSense from index.d.ts.
 *
 * @type {import("../index").Plugin}
 */
module.exports = {
  /**
   * Only business entry. Live prefix hits are invoked on every keystroke
   * while the query starts with a declared command prefix ("demo"); the
   * FULL query including the prefix arrives as args[0]. Keep fast
   * (~150ms budget). Callback actions arrive with the action's own args.
   *
   * @param {string} actionId - command id (live) or callback action id
   * @param {string[]} args - [fullQuery] for live hits, action args otherwise
   */
  onAction(actionId, args) {
    const query = String(args[0] ?? "");
    const term = query.replace(/^\S+\s*/, "");
    return term
      ? [{
          id: "copy-term",
          title: "Copy " + term,
          actions: [{ type: "copy", value: term }]
        }]
      : [];
  },

  /**
   * Optional init hook (~2s budget) and the only lifecycle callback — there
   * is no unload/deactivate hook. Typical use: storage warm-up.
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

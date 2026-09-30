// no_storage.js: activate must NOT see ctx.storage (permission not granted)
// nor ctx.template (Text Snippets is offline) — loading fails loudly if
// either leaks.
module.exports = {
  activate: function (ctx) {
    if (ctx.storage !== undefined) {
      throw new Error("storage must be absent without permission");
    }
    if (ctx.template !== undefined) {
      throw new Error("template must be absent (offline feature)");
    }
  },
  onAction: function () {
    return [];
  }
};

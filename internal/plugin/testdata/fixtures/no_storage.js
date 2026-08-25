// no_storage.js: activate must NOT see ctx.storage when the storage
// permission was not granted — loading fails loudly if it leaks.
module.exports = {
  activate: function (ctx) {
    if (ctx.storage !== undefined) {
      throw new Error("storage must be absent without permission");
    }
  },
  provider: {
    search: function () {
      return [];
    }
  }
};

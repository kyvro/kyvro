// spin_then_return.js: search blocks for ~300ms then returns a result —
// callers with a shorter timeout must get TIMEOUT and drop the late result.
module.exports = {
  provider: {
    search: function () {
      var t0 = Date.now();
      while (Date.now() - t0 < 300) {}
      return [{ id: "late", title: "late", actions: [{ type: "copy", value: "late" }] }];
    }
  }
};

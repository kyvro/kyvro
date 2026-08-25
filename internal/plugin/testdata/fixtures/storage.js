// storage.js: activate() increments a persisted counter; search surfaces
// the current value (proves storage round-trips through JS).
var runs = "0";
module.exports = {
  activate: function (ctx) {
    var n = String(parseInt(ctx.storage.get("runs") || "0", 10) + 1);
    ctx.storage.set("runs", n);
    runs = n;
  },
  provider: {
    search: function () {
      return [{ id: "runs", title: "runs:" + runs, actions: [{ type: "copy", value: runs }] }];
    }
  }
};

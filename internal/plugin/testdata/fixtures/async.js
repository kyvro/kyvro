// async.js: activate returns a Promise; the live call must observe its
// effect (the host awaits settlement before serving calls).
var ready = "no";
module.exports = {
  activate: function () {
    return Promise.resolve().then(function () {
      ready = "yes";
    });
  },
  onAction: function () {
    return [{ id: "ready", title: "ready:" + ready, actions: [{ type: "copy", value: ready }] }];
  }
};

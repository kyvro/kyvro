// async.js: activate returns a Promise; search must observe its effect
// (the host awaits settlement before serving searches).
var ready = "no";
module.exports = {
  activate: function () {
    return Promise.resolve().then(function () {
      ready = "yes";
    });
  },
  provider: {
    search: function () {
      return [{ id: "ready", title: "ready:" + ready, actions: [{ type: "copy", value: ready }] }];
    }
  }
};

// basic.js: a well-formed plugin — command "test.cmd" triggers live via the
// "b64" prefix (full query as args[0]); onAction echoes ids/args and chains
// a callback.
module.exports = {
  activate: function (ctx) {},
  onAction: function (actionId, args) {
    var first = (args && args[0]) || "";
    if (actionId === "back") {
      return [
        {
          id: "a1",
          title: "action:back:" + first,
          actions: [{ type: "copy", value: "c" }]
        }
      ];
    }
    return [
      {
        id: "first",
        title: "P:" + first,
        subtitle: "sub",
        scoreHint: 60,
        actions: [
          { type: "callback", id: "back", args: ["x"] },
          { type: "open-url", url: "https://example.com" },
          { type: "copy", value: "c" }
        ]
      }
    ];
  }
};

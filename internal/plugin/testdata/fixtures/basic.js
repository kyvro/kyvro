// basic.js: a well-formed plugin — provider gated on the "b64" prefix,
// onAction echoing ids/args.
module.exports = {
  activate: function (ctx) {},
  provider: {
    id: "test.provider",
    search: function (query) {
      if (query.indexOf("b64") !== 0) {
        return [];
      }
      return [
        {
          id: "first",
          title: "P:" + query,
          subtitle: "sub",
          scoreHint: 60,
          actions: [{ type: "copy", value: "copied" }]
        }
      ];
    }
  },
  onAction: function (actionId, args) {
    return [
      {
        id: "a1",
        title: "action:" + actionId + ":" + ((args && args[0]) || ""),
        actions: [
          { type: "callback", id: "back", args: ["x"] },
          { type: "open-url", url: "https://example.com" },
          { type: "copy", value: "c" }
        ]
      }
    ];
  }
};

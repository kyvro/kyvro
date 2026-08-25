// bad_entries.js: every invalid entry shape must be dropped without failing
// the valid ones.
module.exports = {
  provider: {
    search: function () {
      return [
        { id: "ok", title: "OK", actions: [{ type: "copy", value: "v" }] },
        { title: "no id", actions: [{ type: "copy", value: "v" }] },
        { id: "notitle", actions: [{ type: "copy", value: "v" }] },
        { id: "noactions", title: "No Actions" },
        { id: "emptyactions", title: "E", actions: [] },
        { id: "badaction", title: "B", actions: [{ type: "nope" }] },
        "not an object",
        { id: "unknownfirst", title: "U", actions: [{ type: "wat" }, { type: "copy", value: "second" }] },
        { id: "score", title: "S", scoreHint: 42, actions: [{ type: "open-url", url: "https://x.example" }] }
      ];
    }
  }
};

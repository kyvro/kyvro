// Kyvro official plugin: GitHub search.
//
// "gh <query>"            → open GitHub repository search for <query>
// "gh owner/repo"         → open https://github.com/owner/repo directly
// "ghost", "gho" etc.     → never routed here (the host command index only
//                           matches the "gh" prefix at a word boundary)
// "gh"                    → routed here, but no argument yet: no rows
//
// No permissions required: rows use host-side open-url actions only.
// Protocol: CommonJS (goja has no ESM support in V1); onAction is the only
// business entry — the host live-calls it on every keystroke while the
// query matches the declared prefix, with the full query as args[0].

module.exports.onAction = function (actionId, args) {
  var query = String((args && args[0]) || "");
  if (actionId !== "github.search") return [];
  // The host gates on the "gh" prefix; here we additionally require a
  // trailing space + input so bare "gh" falls through silently.
  if (query.indexOf("gh ") !== 0) return [];
  var input = query.slice(3).trim();
  if (!input) return [];

  var rows = [];
  if (/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(input)) {
    rows.push({
      id: "repo",
      title: "Open " + input + " on GitHub",
      subtitle: "https://github.com/" + input,
      scoreHint: 35,
      actions: [{ type: "open-url", url: "https://github.com/" + input }]
    });
  }
  rows.push({
    id: "search",
    title: 'Search GitHub for "' + input + '"',
    subtitle: "Repositories matching " + input,
    scoreHint: 30,
    actions: [
      {
        type: "open-url",
        url: "https://github.com/search?q=" + encodeURIComponent(input) + "&type=repositories"
      }
    ]
  });
  return rows;
};

// Kyvro official plugin: GitHub search.
//
// "gh <query>"            → open GitHub repository search for <query>
// "gh owner/repo"         → open https://github.com/owner/repo directly
// "ghost", "gho" etc.     → no results (words merely starting with "gh" are
//                           left to the apps provider)
//
// No permissions required: rows use host-side open-url actions only.
// Protocol: CommonJS (goja has no ESM support in V1).

function results(input) {
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
}

module.exports = {
  provider: {
    id: "kyvro.github",
    search: function (query) {
      // The host gates on the "gh" prefix; here we additionally require a
      // word boundary ("gh" alone or "gh …") so ordinary words starting
      // with "gh" (e.g. "ghost") fall through to the apps provider.
      if (query === "gh") return [];
      if (query.indexOf("gh ") !== 0) return [];
      var input = query.slice(3).trim();
      if (!input) return [];
      return results(input);
    }
  }
};

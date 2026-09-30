// Date Snippets Plugin for Kyvro
//
// Two prefix commands: "date" (today's date rows) and "now" (current time).
// The host live-calls onAction while the query matches the prefix, with the
// full query as args[0]. Also registers a template function for the Text
// Snippets feature when it is available (feature-detected: ctx.template is
// absent while Text Snippets is offline).

function formatDate(date, format) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  const hours = String(date.getHours()).padStart(2, '0');
  const minutes = String(date.getMinutes()).padStart(2, '0');
  const seconds = String(date.getSeconds()).padStart(2, '0');

  return format
    .replace(/YYYY/g, year)
    .replace(/YY/g, String(year).slice(-2))
    .replace(/MM/g, month)
    .replace(/DD/g, day)
    .replace(/HH/g, hours)
    .replace(/mm/g, minutes)
    .replace(/ss/g, seconds);
}

module.exports = {
  activate: function (context) {
    if (context.template) {
      // Usage in Text Snippets: ${date("YYYY-MM-DD")}
      context.template.registerFunc("date", (args) => {
        if (args.length === 0) {
          return new Date().toISOString().split('T')[0];
        }
        return formatDate(new Date(), args[0]);
      });
    }
    context.log.info("Date Snippets plugin activated");
  },

  onAction: function (actionId) {
    const now = new Date();

    if (actionId === "date.today") {
      const today = formatDate(now, "YYYY-MM-DD");
      const todayShort = formatDate(now, "YYMMDD");
      const timestamp = formatDate(now, "YYYYMMDDHHmmss");
      return [
        { id: "date-today-full", title: today, subtitle: "Today's full date (YYYY-MM-DD)", scoreHint: 20, actions: [{ type: "copy", value: today }] },
        { id: "date-today-short", title: todayShort, subtitle: "Today's short date (YYMMDD)", scoreHint: 15, actions: [{ type: "copy", value: todayShort }] },
        { id: "date-timestamp", title: timestamp, subtitle: "Timestamp (YYYYMMDDHHmmss)", scoreHint: 5, actions: [{ type: "copy", value: timestamp }] }
      ];
    }

    if (actionId === "date.now") {
      const timeNow = formatDate(now, "HH:mm:ss");
      return [
        { id: "date-now", title: timeNow, subtitle: "Current time (HH:mm:ss)", scoreHint: 10, actions: [{ type: "copy", value: timeNow }] }
      ];
    }

    return [];
  }
};

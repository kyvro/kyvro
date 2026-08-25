// Date Snippets Plugin for Kyvro
// Demonstrates template function registration for Text Snippets

module.exports.commands = [
  {
    id: "date.today",
    title: "Today's Date",
    keywords: ["today", "date"]
  },
  {
    id: "date.now",
    title: "Current Time",
    keywords: ["now", "time"]
  }
];

// Activate the plugin and register date function
module.exports.activate = (context) => {
  // Register date function for use in Text Snippets
  context.template.registerFunc("date", (args) => {
    if (args.length === 0) {
      return new Date().toISOString().split('T')[0];
    }

    const format = args[0];
    const now = new Date();

    const year = now.getFullYear();
    const month = String(now.getMonth() + 1).padStart(2, '0');
    const day = String(now.getDate()).padStart(2, '0');
    const hours = String(now.getHours()).padStart(2, '0');
    const minutes = String(now.getMinutes()).padStart(2, '0');
    const seconds = String(now.getSeconds()).padStart(2, '0');

    return format
      .replace(/YYYY/g, year)
      .replace(/YY/g, String(year).slice(-2))
      .replace(/MM/g, month)
      .replace(/DD/g, day)
      .replace(/HH/g, hours)
      .replace(/mm/g, minutes)
      .replace(/ss/g, seconds);
  });

  context.log.info("Date Snippets plugin activated");
};

// Format date helper
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

// Provider with direct JavaScript (no template rendering needed)
module.exports.provider = {
  search: async (query) => {
    const results = [];

    if (query.length > 0) {
      const now = new Date();
      const today = formatDate(now, "YYYY-MM-DD");
      const todayShort = formatDate(now, "YYMMDD");
      const timeNow = formatDate(now, "HH:mm:ss");
      const timestamp = formatDate(now, "YYYYMMDDHHmmss");

      results.push({
        id: "date-today-full",
        title: today,
        subtitle: "Today's full date (YYYY-MM-DD)",
        scoreHint: 20,
        action: { kind: "copy", arg: today }
      });

      results.push({
        id: "date-today-short",
        title: todayShort,
        subtitle: "Today's short date (YYMMDD)",
        scoreHint: 15,
        action: { kind: "copy", arg: todayShort }
      });

      results.push({
        id: "date-now",
        title: timeNow,
        subtitle: "Current time (HH:mm:ss)",
        scoreHint: 10,
        action: { kind: "copy", arg: timeNow }
      });

      results.push({
        id: "date-timestamp",
        title: timestamp,
        subtitle: "Timestamp (YYYYMMDDHHmmss)",
        scoreHint: 5,
        action: { kind: "copy", arg: timestamp }
      });
    }

    return results;
  }
};

// Command callback for quick access
module.exports.onCommand = async (commandId) => {
  const now = new Date();

  if (commandId === "date.today") {
    const dateStr = formatDate(now, "YYYY-MM-DD");
    return [{
      id: "today-result",
      title: dateStr,
      subtitle: "Today's date",
      action: { kind: "copy", arg: dateStr }
    }];
  }

  if (commandId === "date.now") {
    const timeStr = formatDate(now, "HH:mm:ss");
    return [{
      id: "now-result",
      title: timeStr,
      subtitle: "Current time",
      action: { kind: "copy", arg: timeStr }
    }];
  }

  return [];
};

// Text Snippets Plugin for Kyvro
// Registers template functions for use in Text Snippets

// Register the date function on activation
module.exports.activate = (context) => {
  // Register date function
  // Usage in Text Snippets: ${date("YYMMDD")}, ${date("YYYY-MM-DD")}
  context.template.registerFunc("date", (args) => {
    if (args.length === 0) {
      return new Date().toISOString().split('T')[0]; // Default: YYYY-MM-DD
    }

    const format = args[0];
    const now = new Date();

    // Format the date according to the format string
    return formatDate(now, format);
  });

  // Register now function (current time)
  // Usage: ${now("HH:mm:ss")}
  context.template.registerFunc("now", (args) => {
    const now = new Date();
    const format = args[0] || "HH:mm:ss";
    return formatDate(now, format);
  });

  // Register today function (today's date)
  // Usage: ${today()}
  context.template.registerFunc("today", (args) => {
    const now = new Date();
    const format = args[0] || "YYYY-MM-DD";
    return formatDate(now, format);
  });

  // Register uuid function
  // Usage: ${uuid()}
  context.template.registerFunc("uuid", (args) => {
    return crypto.randomUUID();
  });

  // Register timestamp function
  // Usage: ${timestamp()}
  context.template.registerFunc("timestamp", (args) => {
    return Date.now().toString();
  });

  context.log.info("Text Snippets plugin activated");
};

// formatDate formats a date according to the given format string
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

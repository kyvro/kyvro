// minimal.js: activate only — no onAction export. Loading must succeed
// (activate-only plugins are valid), but RunAction must report
// INVALID_ARGUMENT.
module.exports = {
  activate: function () {}
};

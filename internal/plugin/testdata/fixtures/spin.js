// spin.js: onAction never terminates — exercises the interrupt path.
module.exports = {
  onAction: function () {
    while (true) {}
  }
};

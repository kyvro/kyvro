// throw.js: onAction throws a JS exception.
module.exports = {
  onAction: function () {
    throw new Error("boom");
  }
};

// throw.js: search throws a JS exception.
module.exports = {
  provider: {
    search: function () {
      throw new Error("boom");
    }
  }
};

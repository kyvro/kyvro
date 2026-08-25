// spin.js: search never terminates — exercises the interrupt path.
module.exports = {
  provider: {
    search: function () {
      while (true) {}
    }
  }
};

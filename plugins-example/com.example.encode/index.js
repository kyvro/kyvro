// Kyvro example plugin: base64 + URL encode/decode.
//
// Protocol: CommonJS (module.exports) — the V1 host runs goja, which has no
// ESM support. Entries: activate(ctx), optional deactivate(), optional
// provider.search(query), onAction(actionId, args).
//
// ctx.storage is only present because the manifest declares the "storage"
// permission; ctx.log.{info,warn,error} is always available.

var B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

function utf8Bytes(str) {
  var bytes = [];
  for (var i = 0; i < str.length; i++) {
    var c = str.codePointAt(i);
    if (c > 0xffff) i++; // skip the low surrogate of the pair
    if (c < 0x80) bytes.push(c);
    else if (c < 0x800) bytes.push(0xc0 | (c >> 6), 0x80 | (c & 63));
    else if (c < 0x10000) bytes.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
    else bytes.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
  }
  return bytes;
}

function utf8Decode(bytes) {
  var s = "";
  for (var i = 0; i < bytes.length; ) {
    var b = bytes[i], cp, len;
    if (b < 0x80) { cp = b; len = 1; }
    else if ((b & 0xe0) === 0xc0) { cp = b & 31; len = 2; }
    else if ((b & 0xf0) === 0xe0) { cp = b & 15; len = 3; }
    else { cp = b & 7; len = 4; }
    for (var j = 1; j < len && i + j < bytes.length; j++) cp = (cp << 6) | (bytes[i + j] & 63);
    s += String.fromCodePoint(cp);
    i += len;
  }
  return s;
}

function base64Encode(str) {
  var bytes = utf8Bytes(str);
  var out = "";
  for (var i = 0; i < bytes.length; i += 3) {
    var b0 = bytes[i];
    var b1 = i + 1 < bytes.length ? bytes[i + 1] : 0;
    var b2 = i + 2 < bytes.length ? bytes[i + 2] : 0;
    out += B64[b0 >> 2];
    out += B64[((b0 & 3) << 4) | (b1 >> 4)];
    out += i + 1 < bytes.length ? B64[((b1 & 15) << 2) | (b2 >> 6)] : "=";
    out += i + 2 < bytes.length ? B64[b2 & 63] : "=";
  }
  return out;
}

function base64Decode(str) {
  str = str.replace(/=+$/, "");
  var bytes = [];
  var buffer = 0, bits = 0;
  for (var i = 0; i < str.length; i++) {
    var v = B64.indexOf(str.charAt(i));
    if (v < 0) return null;
    buffer = (buffer << 6) | v;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      bytes.push((buffer >> bits) & 0xff);
    }
  }
  return utf8Decode(bytes);
}

var activations = "0";

function inputAfter(query, words) {
  var rest = query;
  for (var i = 0; i < words.length; i++) {
    rest = rest.replace(new RegExp("^" + words[i] + "[\\s]*", "i"), "");
  }
  return rest.trim();
}

module.exports = {
  activate: function (ctx) {
    activations = String((parseInt(ctx.storage.get("activations") || "0", 10) || 0) + 1);
    ctx.storage.set("activations", activations);
    ctx.log.info("activated " + activations + " time(s)");
  },

  provider: {
    id: "encode.b64",
    search: function (query) {
      // The host only routes "b64…"-prefixed queries here; keep the guard
      // so the plugin stays correct standalone.
      if (query.indexOf("b64") !== 0) return [];
      var text = query.slice(3).trim();
      if (!text) return [];

      var rows = [
        {
          id: "encode",
          title: base64Encode(text),
          subtitle: "Base64 of " + text + " · Enter to copy",
          scoreHint: 30,
          actions: [{ type: "copy", value: base64Encode(text) }]
        }
      ];
      if (/^[A-Za-z0-9+/]+={0,2}$/.test(text) && text.length % 4 === 0 && base64Decode(text)) {
        rows.push({
          id: "decode",
          title: base64Decode(text),
          subtitle: "Base64-decoded · Enter to copy",
          scoreHint: 25,
          actions: [{ type: "copy", value: base64Decode(text) }]
        });
      }
      return rows;
    }
  },

  onAction: function (actionId, args) {
    if (actionId !== "encode.url") return [];
    // V1 forwards the triggering query as args[0]; strip the command words
    // to recover the text to encode (default to a demo value).
    var input = inputAfter(String((args && args[0]) || ""), ["url", "encode"]) || "hello world";

    var rows = [
      {
        id: "url-encode",
        title: encodeURIComponent(input),
        subtitle: "URL-encoded " + input + " · Enter to copy",
        actions: [{ type: "copy", value: encodeURIComponent(input) }]
      }
    ];
    try {
      var decoded = decodeURIComponent(input);
      if (decoded !== input) {
        rows.push({
          id: "url-decode",
          title: decoded,
          subtitle: "URL-decoded · Enter to copy",
          actions: [{ type: "copy", value: decoded }]
        });
      }
    } catch (e) {
      // not URL-encoded input; skip the decode row
    }
    return rows;
  }
};

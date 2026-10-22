function isExternalURL(value) {
  try {
    const url = new URL(value);
    return url.protocol === 'https:' && !url.username && !url.password;
  } catch { return false; }
}

function sameOrigin(value, origin) {
  try { return !!origin && new URL(value).origin === origin; } catch { return false; }
}

module.exports = { isExternalURL, sameOrigin };

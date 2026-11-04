// Legacy build tools call require('minimatch') directly. Keep that entry point,
// while all matching and parsing use the patched upstream engine.
const engine = require('minimatch');
module.exports = Object.assign(
  (path, pattern, options) => engine.minimatch(path, pattern, options),
  engine,
);

// glob 7 consumes parsed segments through _glob and String#match rather than
// minimatch's public match method. Upstream 11 uses bounded Matcher objects.
class LegacyMinimatch extends engine.Minimatch {
  constructor(pattern, options) {
    super(pattern, options);
    for (const row of this.set) {
      for (const segment of row) {
        if (segment && typeof segment === 'object' && typeof segment.test === 'function') {
          segment._glob = segment.pattern;
          segment[Symbol.match] = text => segment.test(text) ? [text] : null;
        }
      }
    }
  }
}
module.exports.Minimatch = LegacyMinimatch;

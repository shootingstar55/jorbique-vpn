/**
 * Resolves a Wails binding by name at call time.
 *
 * Wails fills window.go when its runtime boots, and only with the methods the current
 * platform binds — several ignore-list methods exist on Windows and not on macOS. Looking
 * the method up per call, rather than destructuring once at module load, is what lets a
 * missing binding degrade into "feature absent" instead of a TypeError. It is the same
 * guard the vanilla code spelled out as `typeof X === 'function'` at every call site.
 *
 * @param {string} name
 * @returns {Function|null}
 */
export function binding(name) {
  const app = window.go?.ui?.App || window.go?.main?.App || {};
  return typeof app[name] === 'function' ? app[name] : null;
}

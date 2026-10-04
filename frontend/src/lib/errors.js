/**
 * Wails rejects with whatever the Go side produced — a string, an Error, or a bare object
 * with .error — so every catch block needs the same coercion before it can show a message.
 * Shared by app.js and the Vue components so both report failures identically.
 */
export function getErrMsg(e, fallback = '') {
  if (!e) return fallback;
  if (typeof e === 'string') return e;
  if (typeof e === 'number' || typeof e === 'boolean') return String(e);
  if (typeof e.message === 'string' && e.message.trim()) return e.message;
  if (typeof e.error === 'string' && e.error.trim()) return e.error;
  try {
    const s = String(e);
    if (s && s !== '[object Object]') return s;
  } catch {
    // Unstringifiable bridge errors still need a safe, user-facing fallback.
    return fallback;
  }
  return fallback;
}

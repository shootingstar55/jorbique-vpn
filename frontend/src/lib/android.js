/**
 * The Android app's stand-in for the Wails bindings.
 *
 * The Android client runs this same interface in a WebView. Its Kotlin side adds a
 * `JorbiqueAndroid` object to the page: `methods()` names what it answers, and
 * `call(id, name, argsJson)` runs one on a background thread and settles it later through
 * window.__jorbiqueSettle. Wrapped here, every method is a function returning a promise —
 * the shape binding() hands out on Windows — so the components need no Android branch to
 * call the backend.
 */

/** True inside the Android app. */
export const isAndroid = typeof window !== 'undefined' && Boolean(window.JorbiqueAndroid);

const pending = new Map();
let nextId = 0;
let app = null;

if (isAndroid) {
  // The stylesheet lets the page fill the phone's screen on this mark.
  document.documentElement.dataset.platform = 'android';
  window.__jorbiqueSettle = (id, ok, payload) => {
    const call = pending.get(id);
    if (!call) return;
    pending.delete(id);
    if (!ok) {
      call.reject(new Error(payload || 'Ошибка'));
      return;
    }
    try {
      call.resolve(payload ? JSON.parse(payload) : undefined);
    } catch (e) {
      call.reject(e);
    }
  };
}

/** The bound methods, by Wails name; null outside the Android app. */
export function androidApp() {
  if (!isAndroid) return null;
  if (app) return app;
  const native = window.JorbiqueAndroid;
  let names = [];
  try {
    names = JSON.parse(native.methods());
  } catch {
    names = [];
  }
  app = {};
  for (const name of names) {
    app[name] = (...args) => new Promise((resolve, reject) => {
      const id = ++nextId;
      pending.set(id, { resolve, reject });
      native.call(id, name, JSON.stringify(args));
    });
  }
  return app;
}

/**
 * Tells the app which theme the page is drawn in, so the status and navigation bars and
 * the window behind the page match it.
 */
export function reportTheme(dark) {
  if (!isAndroid || typeof window.JorbiqueAndroid.setTheme !== 'function') return;
  const background = getComputedStyle(document.body).backgroundColor;
  window.JorbiqueAndroid.setTheme(Boolean(dark), background || '');
}

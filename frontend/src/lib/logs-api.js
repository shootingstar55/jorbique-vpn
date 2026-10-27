import { binding } from './bindings.js';
import { panelAPI } from './panel-api.js';

/**
 * The Logs window's calls. Windows binds them into the window; on macOS the same page is
 * served by the menu bar from a loopback origin, which is what panelAPI speaks.
 */
export const logsAPI = {
  /** @returns {Promise<string[]>} the log files the window can show */
  async files() {
    const list = binding('ListLogFiles');
    if (list) return (await list()) || [];
    return (await panelAPI.load('logs'))?.files || [];
  },
  /**
   * The newest part of a log.
   * @param {string} name
   * @returns {Promise<string>}
   */
  content(name) {
    const get = binding('GetLogContent');
    return get ? get(name) : panelAPI.ask('log-content', { name });
  },
  clear() {
    const clear = binding('ClearLogs');
    return clear ? clear() : panelAPI.ask('clear-logs');
  },
};

/**
 * Puts text on the clipboard. The async Clipboard API is not there on every origin the
 * window is served from (a WKWebView on loopback may not count as secure), so a hidden
 * textarea and execCommand are the fallback.
 * @param {string} text
 */
export async function copyText(text) {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // Fall through to the older way.
    }
  }
  const area = document.createElement('textarea');
  area.value = text;
  area.setAttribute('readonly', '');
  area.style.position = 'fixed';
  area.style.opacity = '0';
  document.body.appendChild(area);
  area.select();
  try {
    if (!document.execCommand('copy')) throw new Error('copy refused');
  } finally {
    area.remove();
  }
}

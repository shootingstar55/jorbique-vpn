import { binding } from './bindings.js';
import { panelAPI } from './panel-api.js';

/**
 * «Личный кабинет»: the account the server keeps for the user's UUID.
 *
 * Windows and Android answer through their bindings (GetAccount, BindThisDevice,
 * UnbindDevice); the macOS main window asks its menu bar through ./api/account. Every
 * call resolves to the same profile: name, uuid, expires, devices_limit, devices and
 * current_device.
 */
const NATIVE = { get: 'GetAccount', bind: 'BindThisDevice', unbind: 'UnbindDevice' };

function call(action, id) {
  const native = binding(NATIVE[action]);
  if (native) return action === 'unbind' ? native(id) : native();
  return panelAPI.ask('account', { action, id });
}

export const accountAPI = {
  get: () => call('get'),
  bindThisDevice: () => call('bind'),
  unbind: (id) => call('unbind', id),
};

/** 2026-01-01 → 1 января 2026; anything unreadable is shown as it came. */
export function formatDate(value) {
  if (!value) return '';
  const date = new Date(/^\d{4}-\d{2}-\d{2}$/.test(value) ? `${value}T00:00:00` : value);
  if (Number.isNaN(date.getTime())) return String(value);
  return date.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' });
}

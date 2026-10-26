import { binding } from './bindings.js';

/**
 * Nicer names for the outbound tags we happen to know; anything else is shown as the server
 * named it. The list of tags itself is never written here — see loadAvailableOutbounds.
 * The null prototype keeps a tag such as constructor or toString from being answered by Object.
 */
const LABELS = {
  __proto__: null,
  'vless-reality': 'VLESS Reality',
  'vless-grpc': 'VLESS gRPC',
  'vless-reality-rmux': 'VLESS Reality · RMUX',
  'vless-xhttp': 'VLESS XHTTP',
  mieru: 'Mieru',
  sudoku: 'Sudoku',
  anytls: 'AnyTLS',
  snell: 'Snell',
  naive: 'NaiveProxy',
};

/** @param {string} tag */
export function outboundLabel(tag) {
  return LABELS[tag] || tag;
}

/**
 * Whether an outbound's toggle starts on: the saved answer, else what the window already showed.
 * An outbound nobody has answered for is on: that is how a protocol the server adds starts
 * working without anyone opening the window. Only the saved map's own keys are answers, so a
 * tag such as constructor is not answered by Object.
 *
 * @param {Record<string, boolean>|null|undefined} stored
 * @param {Record<string, boolean>} previous
 * @param {string} tag
 */
export function outboundChecked(stored, previous, tag) {
  // Object.keys, not Object.hasOwn: macOS 11 ships a WebKit without it.
  const answered = stored && Object.keys(stored).includes(tag);
  return answered ? !!stored[tag] : previous[tag] !== false;
}

/**
 * The outbound tags the downloaded sing-box profile offers.
 *
 * The server decides which protocols exist, so a list written into the client would show what
 * was true when it shipped — a protocol removed months ago, and none of the ones added since.
 * Both the header strip and the config modal read it from here so they cannot disagree.
 *
 * @returns {Promise<string[]>} empty when the profile has not been downloaded yet
 */
export async function loadAvailableOutbounds() {
  const get = binding('GetAvailableOutbounds');
  if (!get) return [];
  try {
    const tags = await get();
    return Array.isArray(tags) ? tags.filter((t) => typeof t === 'string' && t) : [];
  } catch {
    // No profile is available before activation or while the download server is offline.
    return [];
  }
}

/** What each state of a connection check reads as in the protocol list. */
const STATE_NOTES = {
  __proto__: null,
  active: 'активен',
  standby: 'резерв',
  failed: 'недоступен',
  skipped: 'не проверялся',
};

/**
 * The rows of the protocol list: every protocol the profile offers, with what the last
 * connection check made of it while a tunnel is up.
 *
 * The check's own list wins when there is one, since it is what the tunnel was built from;
 * otherwise the profile's tags are shown with no state. Unknown states read as no state.
 *
 * @param {string[]} tags the profile's protocols
 * @param {{ active?: string, protocols?: { tag: string, state: string, latencyMs?: number, speedKiBs?: number }[] }|null} report
 * @returns {{ tag: string, label: string, state: string, note: string, hint: string }[]}
 */
export function protocolRows(tags, report) {
  const checked = Array.isArray(report?.protocols) && report.protocols.length ? report.protocols : null;
  const rows = checked || (tags || []).map((tag) => ({ tag }));
  return rows.filter((row) => row && typeof row.tag === 'string' && row.tag).map((row) => {
    const state = (checked && STATE_NOTES[row.state]) ? row.state : '';
    const note = state ? STATE_NOTES[state] : '';
    const numbers = [];
    if (row.latencyMs > 0) numbers.push(`${row.latencyMs} мс`);
    if (row.speedKiBs > 0) numbers.push(row.speedKiBs >= 1024
      ? `${(row.speedKiBs / 1024).toFixed(1)} МиБ/с`
      : `${Math.round(row.speedKiBs)} КиБ/с`);
    const hint = [outboundLabel(row.tag), note, ...numbers].filter(Boolean).join(' · ');
    return { tag: row.tag, label: outboundLabel(row.tag), state, note, hint };
  });
}

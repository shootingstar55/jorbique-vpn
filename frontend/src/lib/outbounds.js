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
  anytls: 'AnyTLS',
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

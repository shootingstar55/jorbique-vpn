// The first-run UUID screen: what a UUID looks like, and what to tell the user
// when the backend refuses one. Both backends — the Windows app and the macOS
// menu bar — word their refusals the same way, so one mapping serves both.

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function looksLikeUUID(value) {
  return uuidPattern.test(String(value || '').trim());
}

export function explainActivationError(message) {
  const text = String(message || '');
  if (/invalid user uuid/i.test(text)) return 'That is not a UUID. It looks like xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.';
  if (/not registered|unknown user uuid/i.test(text)) return 'The server does not know this UUID. Check it and try again.';
  if (/daemon, which is not running|service is not running/i.test(text)) return 'The Jorbique VPN Extended service is not running. Reinstall the app and try again.';
  if (/reach the server|server unreachable/i.test(text)) return 'Could not reach the server. Check the internet connection and try again.';
  return text || 'Could not check the UUID. Try again.';
}

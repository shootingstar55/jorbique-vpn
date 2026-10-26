// The first-run UUID screen: what a UUID looks like, and what to tell the user
// when the backend refuses one. Both backends — the Windows app and the macOS
// menu bar — word their refusals the same way, so one mapping serves both.

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function looksLikeUUID(value) {
  return uuidPattern.test(String(value || '').trim());
}

export function explainActivationError(message) {
  const text = String(message || '');
  if (/invalid user uuid|неверный формат uuid/i.test(text)) return 'Это не UUID. Он выглядит так: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.';
  if (/not registered|unknown user uuid|не зарегистрирован/i.test(text)) return 'Сервер не знает этот UUID. Проверьте его и попробуйте ещё раз.';
  if (/daemon, which is not running|service is not running/i.test(text)) return 'Служба Jorbique VPN Extended не запущена. Переустановите приложение и попробуйте ещё раз.';
  if (/reach the server|server unreachable|связаться с сервером/i.test(text)) return 'Не удалось связаться с сервером. Проверьте подключение к интернету и попробуйте ещё раз.';
  return text || 'Не удалось проверить UUID. Попробуйте ещё раз.';
}

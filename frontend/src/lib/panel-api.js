/**
 * The bridge a standalone panel talks to.
 *
 * On macOS every panel is a Vue page in a WKWebView, served by a private
 * loopback origin the daemon starts for that window: GET reads the values the
 * window opened with, POST saves them, and POST ./api/close is the Cancel
 * button reaching the window it is drawn in. On Windows the same components run
 * inside the app and use their Wails bindings instead, so nothing here is
 * reached.
 */
async function request(path, method, body) {
  const response = await fetch(`./api/${path}`, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) throw new Error((await response.text()) || `HTTP ${response.status}`);
  if (response.status === 204) return undefined;
  return response.json();
}

export const panelAPI = {
  load: (path) => request(path, 'GET'),
  save: (path, body) => request(path, 'POST', body),
  /** An action with an answer, as opposed to save's yes/no. */
  ask: (path, body) => request(path, 'POST', body ?? {}),
  close: () => request('close', 'POST', []).catch(() => {}),
};

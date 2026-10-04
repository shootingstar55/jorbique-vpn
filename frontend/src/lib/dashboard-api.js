import { binding } from './bindings.js';
import { panelAPI } from './panel-api.js';

// Windows binds these methods into the window; macOS serves the same page from
// the daemon's loopback origin, which is what panelAPI already speaks.
function call(bindingName, path, body) {
  const native = binding(bindingName);
  if (native) return body ? native(...body) : native();
  return body ? panelAPI.save(path, body) : panelAPI.load(path);
}

export const dashboardAPI = {
  snapshot: () => call('DashboardSnapshot', 'snapshot'),
  select: (group, outbound) => call('DashboardSelectOutbound', 'select', [group, outbound]),
  urlTest: (outbound) => call('DashboardURLTest', 'url-test', [outbound]),
};

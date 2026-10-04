import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';

// Exercise the actual picker handler without opening a native file chooser.
const source = readFileSync(new URL('./RouteRulesPanel.vue', import.meta.url), 'utf8')
  .match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*;$/gm, '');
const groups = [
  { id: '__webkit_networking__', paths: ['/System/com.apple.WebKit.Networking'] },
  { id: '/Applications/Test.app', paths: ['/Applications/Test.app/Contents/MacOS/Test', '/Applications/Test.app/Contents/MacOS/Helper'] },
];
let sent;
const context = {
  ref: (value) => ({ value }), onMounted: () => {},
  getErrMsg: (error) => { throw error; },
  panelAPI: { save: async (path, body) => { sent = { path, body }; return { apps: groups }; } },
};
runInNewContext(`${source}\nglobalThis.handler = { apps, addApplications };`, context);
context.handler.apps.value = groups;
await context.handler.addApplications();
assert.equal(sent.path, 'pick-apps');
assert.deepEqual(sent.body, groups.flatMap((app) => app.paths));
console.log('RouteRulesPanel preserves executable paths');

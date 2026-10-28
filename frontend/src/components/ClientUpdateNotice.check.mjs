import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
const source = readFileSync(new URL('./ClientUpdateNotice.vue', import.meta.url), 'utf8')
  .match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*;$/gm, '');
const messages = [];
let checkResult = async () => ({ currentVersion: '1.0.0', version: '1.1.0', available: true });
let settle;
let installs = 0;
let mounted, unmounted, interval, progressInterval, delay;
const cleared = [];
let checks = 0;
const context = {
  appVersion: '1.0.0\n',
  ref: value => ({ value }), onMounted: fn => { mounted = fn; }, onBeforeUnmount: fn => { unmounted = fn; },
  computed: fn => ({ get value() { return fn(); } }),
  setInterval: (fn, ms) => { if (ms === 250) { progressInterval = fn; return 43; } interval = fn; delay = ms; return 42; },
  clearInterval: id => { cleared.push(id); },
  useMessage: () => Object.fromEntries(['info', 'error', 'success'].map(type => [type, value => messages.push([type, value])])),
  useDialog: () => ({}), getErrMsg: (e, fallback) => e.message || fallback,
  binding: name => ({
    CheckClientUpdate: async () => { checks++; return checkResult(); },
    GetClientUpdateProgress: async () => ({ stage: 'downloading', downloaded: 5, total: 10 }),
    InstallClientUpdate: async () => { installs++; return await new Promise(resolve => { settle = resolve; }); },
  }[name]),
};
runInNewContext(`${source}\nglobalThis.handlers = { check, install, status, checking, installing, percent, progressLabel };`, context);
const { check, install, status } = context.handlers;
assert.equal(status.value.currentVersion, '1.0.0', 'installed version is known without a network request');
mounted();
await new Promise(resolve => setImmediate(resolve));
assert.equal(checks, 1, 'one automatic check at startup');
assert.equal(delay, 6 * 60 * 60 * 1000, 'recheck every six hours');
assert.equal(status.value.available, true);
assert.equal(messages.length, 0, 'background checks stay quiet');
checkResult = async () => ({ currentVersion: '1.0.0', version: '1.0.0', available: false });
await interval();
assert.equal(checks, 2);
assert.equal(status.value.available, false, 'equal versions do not offer an update');
assert.equal(messages.length, 0, 'up-to-date checks stay quiet');
checkResult = async () => ({ currentVersion: '1.0.0', version: '1.1.0', available: true });
await check();
checkResult = async () => { throw new Error('offline'); };
await check();
assert.equal(messages.length, 0, 'network failures must not interrupt the client');
assert.equal(status.value.currentVersion, '1.0.0', 'offline checks preserve the installed version');
assert.equal(status.value.version, '', 'failed checks do not present an old result as current');
assert.equal(status.value.available, false);
const pending = install();
await install();
assert.equal(installs, 1, 'double click must not start two installers');
assert.equal(context.handlers.installing.value, true);
await progressInterval();
assert.equal(context.handlers.percent.value, 50);
assert.match(context.handlers.progressLabel.value, /50%/);
settle();
await pending;
assert.equal(context.handlers.installing.value, false);
const restarting = install();
settle(true);
await restarting;
assert.equal(context.handlers.installing.value, true, 'keep progress visible until Windows exits');
assert.match(context.handlers.progressLabel.value, /Перезапускаем/);
assert.ok(cleared.includes(43), 'stop progress polling once the installer takes over');
unmounted();
assert.ok(cleared.includes(42), 'stop polling when the view is destroyed');
console.log('Client updates: startup and six-hour checks, version status, equal versions, offline checks, single installer');

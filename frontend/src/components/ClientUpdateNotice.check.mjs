import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
const source = readFileSync(new URL('./ClientUpdateNotice.vue', import.meta.url), 'utf8')
  .match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*;$/gm, '');
const messages = [];
let checkResult = async () => ({ currentVersion: '1.0.0', version: '1.1.0', available: true });
let settle;
let installs = 0;
const context = {
  ref: value => ({ value }), onMounted: () => {}, onBeforeUnmount: () => {},
  useMessage: () => Object.fromEntries(['info', 'error', 'success'].map(type => [type, value => messages.push([type, value])])),
  useDialog: () => ({}), getErrMsg: (e, fallback) => e.message || fallback,
  binding: name => name === 'CheckClientUpdate' ? checkResult : async () => { installs++; await new Promise(resolve => { settle = resolve; }); },
};
runInNewContext(`${source}\nglobalThis.handlers = { check, install, status, checking, installing };`, context);
const { check, install, status } = context.handlers;
await check();
assert.equal(status.value.available, true);
assert.equal(messages.length, 0, 'background checks stay quiet');
checkResult = async () => { throw new Error('offline'); };
await check();
assert.equal(messages.length, 0, 'network failures must not interrupt the client');
await check(true);
assert.equal(messages[0][0], 'error');
const pending = install();
await install();
assert.equal(installs, 1, 'double click must not start two installers');
assert.equal(context.handlers.installing.value, true);
settle();
await pending;
assert.equal(context.handlers.installing.value, false);
console.log('Client updates: quiet checks, visible manual errors, single installer');

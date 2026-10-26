import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { getErrMsg } from '../lib/errors.js';
import { looksLikeUUID } from '../lib/activation.js';

// Exercise the component handlers with a failed native read, then a successful retry.
// A missing response must never become a new empty/default config on disk.
function loadHandlers(file, bindings, names) {
  const events = [];
  const source = readFileSync(new URL(file, import.meta.url), 'utf8')
    .match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*;$/gm, '');
  const context = {
    ref: (value) => ({ value }), computed: (get) => ({ get value() { return get(); } }),
    watch: () => {}, onMounted: () => {}, onBeforeUnmount: () => {}, nextTick: async () => {},
    defineProps: () => ({ standalone: false }), defineEmits: () => (...args) => events.push(args),
    useMessage: () => ({}), clearTimeout, setTimeout,
    binding: (name) => bindings[name], getErrMsg, looksLikeUUID, loadAvailableOutbounds: async () => [],
  };
  runInNewContext(`${source}\nglobalThis.handlers = { ${names} };`, context);
  return { ...context.handlers, events };
}

const fail = async () => { throw new Error('Read unavailable'); };
const writes = [];
const bindings = {
  GetIgnoredProcessPaths: fail,
  GetIgnoredProcessNames: async () => ['saved.exe'],
  GetDirectDomains: async () => 'saved.example',
  SetIgnoredProcessNamesAndPaths: async (...args) => writes.push(args),
  SetDirectDomains: async (...args) => writes.push(args),
};
const ignore = loadHandlers('./IgnoreAppsModal.vue', bindings, 'load, loadDomains, save, close, hydrated');
await ignore.load();
await ignore.loadDomains();
await ignore.save();
assert.equal(ignore.hydrated.value, false);
assert.equal(writes.length, 0, 'a failed path read must block every config write');
assert.equal(ignore.events[0][0], 'error');

bindings.GetIgnoredProcessPaths = async () => ['/saved.exe'];
await ignore.load();
await ignore.save();
assert.equal(writes.length, 2);
assert.deepEqual(Array.from(writes[0][0]), ['saved.exe']);
assert.deepEqual(Array.from(writes[0][1]), ['/saved.exe']);
assert.equal(writes[1][0], 'saved.example');

await ignore.load();
bindings.GetDirectDomains = fail;
await ignore.loadDomains();
const savedWrites = writes.length;
await ignore.save();
assert.equal(writes.length, savedWrites, 'unreadable domains must not be overwritten');
bindings.GetDirectDomains = async () => 'saved.example';
await ignore.loadDomains();
bindings.SetIgnoredProcessNamesAndPaths = fail;
ignore.events.length = 0;
await ignore.close();
assert.equal(ignore.hydrated.value, false, 'a failed pre-close save still closes the window');
assert.ok(ignore.events.some(([event]) => event === 'error'), 'a failed pre-close save is reported');
assert.ok(ignore.events.some(([event]) => event === 'update:visible'));

const configBindings = {
  GetUserUUID: fail,
  SetUserUUID: async (...args) => writes.push(args),
};
const config = loadHandlers('./UuidModal.vue', configBindings, 'load, save, hydrated');
const before = writes.length;
await config.load();
await config.save();
assert.equal(config.hydrated.value, false);
assert.equal(writes.length, before, 'an unreadable config must not be overwritten');
const savedUUID = '11111111-1111-4111-8111-111111111111';
configBindings.GetUserUUID = async () => savedUUID;
await config.load();
assert.equal(config.hydrated.value, true);
await config.save();
assert.equal(writes.at(-1)[0], savedUUID, 'the saved UUID survives retry');

assert.equal(getErrMsg({ toString() { throw new Error('not printable'); } }, 'Fallback'), 'Fallback');
console.log('Config reads fail closed; saved ignore rules and the account survive retry');

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { explainActivationError, looksLikeUUID } from '../lib/activation.js';
import { getErrMsg } from '../lib/errors.js';

const uuid = '11111111-2222-4333-8444-555555555555';
const source = file => readFileSync(new URL(file, import.meta.url), 'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*;$/gm, '');
const messages = [];
let imports = 0, failure = null, reconnects = 0, link = uuid, stored = 'previous-account';
const bindings = {
  TakePendingImportUUID: async () => link,
  GetUserUUID: async () => stored,
  ImportUserUUID: async value => { imports++; assert.equal(value, uuid); if (failure) throw failure; stored = value; },
};
const context = {
  ref: value => ({ value }), onMounted() {}, onUnmounted() {}, nextTick: async () => {},
  useMessage: () => Object.fromEntries(['success', 'error', 'info'].map(type => [type, text => messages.push([type, text])])),
  binding: name => bindings[name], explainActivationError, looksLikeUUID, getErrMsg,
};
runInNewContext(source('./AppShell.vue') + '\nglobalThis.h = { takeImportLink, activated, importUuid, mainScreen };', context);
const h = context.h;
h.activated.value = false;
await h.takeImportLink();
assert.equal(h.importUuid.value, uuid, 'first import submits the activation screen');
assert.equal(imports, 0);
const presets = [];
let preset = h.importUuid.value;
Object.defineProperty(h.importUuid, 'value', { get: () => preset, set: value => { presets.push(value); preset = value; } });
await h.takeImportLink();
assert.deepEqual(presets, ['', uuid], 'reopening a failed import of the same UUID must retry activation');
h.activated.value = true;
h.mainScreen.value = { reconnectIfConnected() { reconnects++; } };
await h.takeImportLink();
await h.takeImportLink();
assert.equal(imports, 2, 'the same UUID must also request binding again');
assert.equal(reconnects, 1, 'reopening the same UUID must not interrupt the VPN');
assert.ok(messages.every(([kind]) => kind === 'success'));
failure = new Error('Достигнут лимит устройств.');
messages.length = 0;
await h.takeImportLink();
assert.equal(reconnects, 1, 'a refused import must not reconnect');
assert.equal(messages[0][0], 'error');
assert.match(messages[0][1], /на странице подписки/);
link = 'not-a-uuid';
const before = imports;
await h.takeImportLink();
assert.equal(imports, before, 'malformed links never reach the account API');

for (const presetUuid of [uuid, '']) {
  let chosen, emitted = false;
  const activation = {
    ref: value => ({ value }), watch() {}, defineProps: () => ({ standalone: false, presetUuid }),
    defineEmits: () => () => { emitted = true; },
    binding: name => async value => { chosen = name; assert.equal(value, uuid); if (failure) throw failure; },
    explainActivationError, looksLikeUUID, getErrMsg,
  };
  runInNewContext(source('./ActivationScreen.vue') + '\nglobalThis.h = { uuid, activate, error };', activation);
  activation.h.uuid.value = uuid;
  failure = null;
  await activation.h.activate();
  assert.equal(chosen, presetUuid ? 'ImportUserUUID' : 'ActivateUserUUID');
  assert.equal(emitted, true);
  emitted = false;
  failure = new Error('Достигнут лимит устройств.');
  await activation.h.activate();
  assert.equal(emitted, false, 'do not complete activation when binding fails');
  assert.match(activation.h.error.value, /на странице подписки/);
}
console.log('UUID imports: first launch, existing account, repeated UUID, device limit and invalid links');

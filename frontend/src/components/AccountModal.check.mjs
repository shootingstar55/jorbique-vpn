import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { getErrMsg } from '../lib/errors.js';

const source = readFileSync(new URL('./AccountModal.vue', import.meta.url), 'utf8')
  .match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*;$/gm, '');
let visible;
const props = { visible: false };
const events = [];
let answer = async () => ({ uuid: 'first-account', devices: [] });
let confirmation;
const context = {
  ref: value => ({ value }), computed: get => ({ get value() { return get(); } }),
  watch: (get, callback) => { visible = callback; },
  defineProps: () => props, defineEmits: () => (...args) => events.push(args),
  useDialog: () => ({ warning: value => { confirmation = value; } }),
  accountAPI: { get: () => answer() }, getErrMsg, labels: { cancel: 'Отмена' },
};
runInNewContext(`${source}\nglobalThis.handlers = { load, run, profile, unbind };`, context);
const { load, run, profile, unbind } = context.handlers;

unbind({ name: 'iPhone', source: 'sub' });
assert.match(confirmation.content, /обновит подписку/, 'subscription devices can reclaim a free slot');

await load();
assert.equal(profile.value.uuid, 'first-account');
props.visible = false;
visible(false);
assert.equal(profile.value, null, 'closing must discard account credentials');
answer = async () => { throw new Error('offline'); };
props.visible = true;
visible(true);
await Promise.resolve();
assert.equal(profile.value, null, 'a failed read must not show the previous UUID');

let settle;
const old = run('unbind', () => new Promise(resolve => { settle = resolve; }), 'done');
props.visible = false;
visible(false);
answer = async () => ({ uuid: 'new-account', devices: [] });
props.visible = true;
visible(true);
await Promise.resolve();
settle({ uuid: 'first-account' });
await old;
assert.equal(profile.value.uuid, 'new-account', 'a late mutation must not restore the previous account');
assert.equal(events.length, 0, 'closed account actions must not emit stale notifications');
console.log('Account modal discards old credentials and ignores stale responses');

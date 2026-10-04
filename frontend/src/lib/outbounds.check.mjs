// The server names the outbounds, so a tag may be anything, including a name
// Object's prototype answers for; those must come back as the server wrote them.
import assert from 'node:assert/strict';
import { outboundChecked, outboundLabel } from './outbounds.js';

assert.equal(outboundLabel('vless-reality'), 'VLESS Reality');
assert.equal(outboundLabel('tuic'), 'TUIC');
for (const tag of ['new-protocol', 'constructor', 'toString', '__proto__', 'hasOwnProperty', 'valueOf']) {
  assert.equal(outboundLabel(tag), tag);
}

// A new tag nobody answered for is on; a saved answer wins over what was shown.
assert.equal(outboundChecked({}, {}, 'new-protocol'), true);
assert.equal(outboundChecked({ tuic: false }, { tuic: true }, 'tuic'), false);
assert.equal(outboundChecked({ tuic: true }, { tuic: false }, 'tuic'), true);
assert.equal(outboundChecked(null, { tuic: false }, 'tuic'), false);
assert.equal(outboundChecked({}, {}, 'constructor'), true);
// Only the map's own keys are answers: an inherited constructor is not one.
assert.equal(outboundChecked({}, { constructor: false }, 'constructor'), false);
console.log('outbounds ok');

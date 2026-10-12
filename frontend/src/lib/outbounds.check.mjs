// The server names the outbounds, so a tag may be anything, including a name
// Object's prototype answers for; those must come back as the server wrote them.
import assert from 'node:assert/strict';
import { outboundChecked, outboundLabel } from './outbounds.js';

assert.equal(outboundLabel('vless-reality'), 'VLESS Reality');
assert.equal(outboundLabel('anytls'), 'AnyTLS');
for (const tag of ['new-protocol', 'constructor', 'toString', '__proto__', 'hasOwnProperty', 'valueOf']) {
  assert.equal(outboundLabel(tag), tag);
}

// A new tag nobody answered for is on; a saved answer wins over what was shown.
assert.equal(outboundChecked({}, {}, 'new-protocol'), true);
assert.equal(outboundChecked({ anytls: false }, { anytls: true }, 'anytls'), false);
assert.equal(outboundChecked({ anytls: true }, { anytls: false }, 'anytls'), true);
assert.equal(outboundChecked(null, { anytls: false }, 'anytls'), false);
assert.equal(outboundChecked({}, {}, 'constructor'), true);
// Only the map's own keys are answers: an inherited constructor is not one.
assert.equal(outboundChecked({}, { constructor: false }, 'constructor'), false);
console.log('outbounds ok');

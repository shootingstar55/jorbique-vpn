// The server names the outbounds, so a tag may be anything, including a name
// Object's prototype answers for; those must come back as the server wrote them.
import assert from 'node:assert/strict';
import { outboundChecked, outboundLabel, protocolRows } from './outbounds.js';

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

// Off: the profile's protocols, with no state.
assert.deepEqual(protocolRows(['vless-reality', 'snell'], null).map((r) => [r.label, r.state]), [['VLESS Reality', ''], ['Snell', '']]);
assert.deepEqual(protocolRows(null, null), []);
// Connected: the check's own list and its outcomes, numbers in the hint.
const rows = protocolRows(['ignored'], {
  active: 'anytls',
  protocols: [
    { tag: 'anytls', state: 'active', latencyMs: 42, speedKiBs: 2048 },
    { tag: 'mieru', state: 'standby', latencyMs: 90, speedKiBs: 300.4 },
    { tag: 'sudoku', state: 'failed' },
    { tag: 'naive', state: 'skipped' },
    { tag: 'odd', state: 'constructor' },
  ],
});
assert.deepEqual(rows.map((r) => r.state), ['active', 'standby', 'failed', 'skipped', '']);
assert.equal(rows[0].hint, 'AnyTLS · активен · 42 мс · 2.0 МиБ/с');
assert.equal(rows[1].hint, 'Mieru · резерв · 90 мс · 300 КиБ/с');
assert.equal(rows[2].note, 'недоступен');
// An empty report is no report.
assert.deepEqual(protocolRows(['a'], { protocols: [] }).map((r) => r.tag), ['a']);
console.log('outbounds ok');

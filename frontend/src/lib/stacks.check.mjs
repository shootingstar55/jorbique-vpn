// One runnable check for the label rules: the three spellings the value arrives
// in, and a name from a client this one has not met.
import assert from 'node:assert/strict';
import { stackLabel } from './stacks.js';

assert.equal(stackLabel('system'), 'System');       // the macOS config
assert.equal(stackLabel('tun_gvisor'), 'Gvisor');   // a running profile file
assert.equal(stackLabel('Mixed'), 'Mixed');         // already a label
assert.equal(stackLabel(''), '');
assert.equal(stackLabel('newstack'), 'Newstack');   // unknown, still readable
console.log('stackLabel ok');

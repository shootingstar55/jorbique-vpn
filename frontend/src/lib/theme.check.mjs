import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// The light theme takes the colours out of the overrides and drops the rest, so
// anything in there that sets a size applies to the dark theme alone and the two
// windows lay out differently. Keys, not values: a box shadow may name a px.
const source = readFileSync(new URL('./theme.js', import.meta.url), 'utf8');
const overrides = source.slice(source.indexOf('const overrides = {'), source.indexOf('\n};'));
const layout = [...overrides.matchAll(/^\s*(\w+):/gm)]
  .map(([, key]) => key)
  .filter((key) => /^(border)?[Rr]adius|adding|[Hh]eight|[Ww]idth|[Ff]ontSize|[Ff]ontWeight|[Ff]ontFamily|^size/.test(key));

assert.deepEqual(layout, [], `overrides must carry colours only; these change the layout: ${layout}`);
console.log('theme overrides carry no layout');

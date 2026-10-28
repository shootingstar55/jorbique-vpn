import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';

// The light theme takes the colours out of the overrides and drops the rest, so
// anything in there that sets a size applies to the dark theme alone and the two
// windows lay out differently. Keys, not values: a box shadow may name a px.
const source = readFileSync(new URL('./theme.js', import.meta.url), 'utf8');
const overrides = source.slice(source.indexOf('const overrides = {'), source.indexOf('\n};'));
const layout = [...overrides.matchAll(/^\s*(\w+):/gm)]
  .map(([, key]) => key)
  .filter((key) => /^(border)?[Rr]adius|adding|[Hh]eight|[Ww]idth|[Ff]ontSize|[Ff]ontWeight|[Ff]ontFamily|^size/.test(key));

assert.deepEqual(layout, [], `overrides must carry colours only; these change the layout: ${layout}`);

const computed = get => ({ get value() { return get(); } });
function loadTheme(saved, query = '') {
  const storage = new Map(saved == null ? [] : [['jorbiquevpn.theme', saved]]);
  const context = {
    computed, ref: value => ({ value }), darkTheme: {}, lightTheme: {}, URLSearchParams,
    window: { location: { search: query }, matchMedia: () => { throw Error('Theme must not follow the OS'); } },
    localStorage: { getItem: key => storage.get(key), setItem: (key, value) => storage.set(key, value) },
  };
  runInNewContext(source.replace(/^import .*;$/gm, '').replace(/^export /gm, '')
    + '\nglobalThis.theme = { themeMode, isDark, setThemeMode };', context);
  return { ...context.theme, storage };
}
const theme = loadTheme('system', '?theme=system');
assert.equal(theme.themeMode.value, 'dark', 'legacy automatic preference becomes dark');
assert.equal(theme.isDark.value, true);
theme.setThemeMode('light');
assert.equal(theme.isDark.value, false);
assert.equal(theme.storage.get('jorbiquevpn.theme'), 'light');
theme.setThemeMode('system');
assert.equal(theme.themeMode.value, 'light', 'automatic mode is no longer accepted');
assert.equal(loadTheme('light').themeMode.value, 'light', 'saved explicit choice survives restart');
assert.equal(loadTheme('light', '?theme=dark').themeMode.value, 'dark', 'macOS window preference wins');
assert.equal(loadTheme(null).themeMode.value, 'dark');

const changes = [];
const switchContext = { ...theme, computed, defineOptions: () => {}, defineEmits: () => (_, mode) => changes.push(mode) };
const toggle = readFileSync(new URL('../components/ThemeSwitch.vue', import.meta.url), 'utf8')
  .match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*;$/gm, '');
runInNewContext(toggle + '\nglobalThis.nextTheme = next;', switchContext);
for (let i = 0; i < 4; i++) switchContext.nextTheme();
assert.deepEqual(changes, ['dark', 'light', 'dark', 'light'], 'each click switches to the other explicit theme');
console.log('Theme: shared layout, two-mode toggle, persistence, legacy preferences and macOS windows');

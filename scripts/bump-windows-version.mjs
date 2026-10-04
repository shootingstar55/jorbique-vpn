#!/usr/bin/env node
/**
 * Sync wails.json and winres/winres.json productVersion from APP_VERSION.
 * Used by scripts/build-windows.ps1 (and optionally CI).
 */
import fs from 'node:fs';

const v = (process.env.APP_VERSION || '').trim();
if (!v) {
  console.error('APP_VERSION is required');
  process.exit(1);
}
// These strings become the PE version resource, so nothing but x.y.z may pass.
// The build scripts resolve APP_VERSION through scripts/app-version.sh, which
// checks the same shape; this is the guard for running the script by hand.
if (!/^\d+\.\d+\.\d+$/.test(v)) {
  console.error(`APP_VERSION=${v} is not a bare x.y.z`);
  process.exit(1);
}

const w = JSON.parse(fs.readFileSync('wails.json', 'utf8'));
w.info.productVersion = v;
fs.writeFileSync('wails.json', JSON.stringify(w, null, 2) + '\n');

const wr = JSON.parse(fs.readFileSync('winres/winres.json', 'utf8'));
const fv = `${v}.0`;
wr.RT_MANIFEST['#1']['0409'].identity.version = v;
wr.RT_VERSION['#1']['0000'].fixed.file_version = fv;
wr.RT_VERSION['#1']['0000'].fixed.product_version = fv;
wr.RT_VERSION['#1']['0000'].info['0409'].FileVersion = v;
wr.RT_VERSION['#1']['0000'].info['0409'].ProductVersion = v;
fs.writeFileSync('winres/winres.json', JSON.stringify(wr, null, 2) + '\n');

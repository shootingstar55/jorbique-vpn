import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

export default defineConfig({
  plugins: [vue()],
  // Wails serves dist/ out of the Go embed FS, so asset URLs have to be relative:
  // an absolute /assets/... would be resolved against the asset server root and 404.
  base: './',
  // assets/ holds app-icon.png, which index.html loads by name, and the flags. Pointing the
  // public dir at it copies them to dist/ verbatim and replaces the hand-rolled
  // copyFileSync the old npm script did.
  publicDir: '../assets',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // The default 500 kB warning is about download cost over a network. This bundle is
    // embedded in the executable and served from memory by the Wails asset server, so the
    // size that matters is the binary's, not a transfer's — splitting it would only add
    // requests. Raised so a real regression, rather than Naive UI's baseline, is what warns.
    chunkSizeWarningLimit: 900,
  },
});

<script setup>
import { computed } from 'vue';

/**
 * The icon set, inline.
 *
 * Naive UI ships no icons of its own — it expects an icon pack alongside it. Rather than add
 * one for the dozen glyphs this app uses, each is a stroked 24x24 path list drawn here and
 * wrapped in NIcon at the call site.
 */
const PATHS = {
  power: ['M12 3v9', 'M7.5 6.2a8 8 0 1 0 9 0'],
  shield: ['M12 3l7.5 3v5.5c0 4.5-3.1 7.9-7.5 9.2-4.4-1.3-7.5-4.7-7.5-9.2V6z'],
  shieldCheck: ['M12 3l7.5 3v5.5c0 4.5-3.1 7.9-7.5 9.2-4.4-1.3-7.5-4.7-7.5-9.2V6z', 'M9 12l2 2 4-4'],
  settings: [
    'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
    'M19.4 15a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-1.8-.3 1.6 1.6 0 0 ' +
    '0-1 1.5v.2a2 2 0 1 1-4 0v-.1a1.6 1.6 0 0 0-1-1.5 1.6 1.6 0 0 0-1.8.3l-.1.1a2 2 0 1 ' +
    '1-2.8-2.8l.1-.1a1.6 1.6 0 0 0 .3-1.8 1.6 1.6 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.6 1.6 0 0 0 1.5-1' +
    ' 1.6 1.6 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.6 1.6 0 0 0 1.8.3H9a1.6 1.6 0 0 0 ' +
    '1-1.5V3a2 2 0 1 1 4 0v.1a1.6 1.6 0 0 0 1 1.5 1.6 1.6 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 ' +
    '2.8l-.1.1a1.6 1.6 0 0 0-.3 1.8V9a1.6 1.6 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.6 1.6 0 0 0-1.5 1z',
  ],
  apps: ['M4 4h7v7H4z', 'M13 4h7v7h-7z', 'M4 13h7v7H4z', 'M13 13h7v7h-7z'],
  logs: ['M4 5h16', 'M4 10h16', 'M4 15h11', 'M4 20h7'],
  monitor: ['M3 5h18v11H3z', 'M9 20h6', 'M12 16v4'],
  key: ['M15 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6z', 'M12.2 12H3', 'M6 12v3', 'M9 12v2'],
  refresh: ['M20 11a8 8 0 1 0-1.6 5.4', 'M20 5v6h-6'],
  plus: ['M12 5v14', 'M5 12h14'],
  trash: ['M4 7h16', 'M9 7V5h6v2', 'M6 7l1 13h10l1-13'],
  copy: ['M9 9h10v10H9z', 'M5 15V5h10'],
  grip: ['M9 6h.01', 'M9 12h.01', 'M9 18h.01', 'M15 6h.01', 'M15 12h.01', 'M15 18h.01'],
  close: ['M6 6l12 12', 'M18 6L6 18'],
  browser: ['M3 12a9 9 0 1 0 18 0 9 9 0 0 0-18 0Z', 'M3 12h18', 'M12 3a14 14 0 0 1 0 18', 'M12 3a14 14 0 0 0 0 18'],
  chat: ['M21 15a4 4 0 0 1-4 4H8l-5 3V7a4 4 0 0 1 4-4h10a4 4 0 0 1 4 4z'],
  music: ['M9 18V6l12-2v12', 'M9 18a3 3 0 1 1-6 0 3 3 0 0 1 6 0z', 'M21 16a3 3 0 1 1-6 0 3 3 0 0 1 6 0z'],
  code: ['M8 9 5 12l3 3', 'M16 9l3 3-3 3', 'M14 7 10 17'],
  app: ['M12 2 3 7v10l9 5 9-5V7z', 'M12 22V12', 'M21 7l-9 5-9-5'],
  sun: [
    'M12 16a4 4 0 1 0 0-8 4 4 0 0 0 0 8z',
    'M12 2v2', 'M12 20v2', 'M4.9 4.9l1.4 1.4', 'M17.7 17.7l1.4 1.4',
    'M2 12h2', 'M20 12h2', 'M4.9 19.1l1.4-1.4', 'M17.7 6.3l1.4-1.4',
  ],
  moon: ['M20.5 14.5A8.5 8.5 0 0 1 9.5 3.5a8.5 8.5 0 1 0 11 11z'],
  // A circle half filled in: the system decides which half wins.
  themeAuto: ['M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z', 'M12 3v18', 'M12 7h4.5', 'M12 11h7.5', 'M12 15h7', 'M12 19h3.5'],
};

const props = defineProps({
  name: { type: String, required: true },
});

const paths = computed(() => PATHS[props.name] || PATHS.app);
</script>

<template>
  <svg
    viewBox="0 0 24 24" fill="none" stroke="currentColor"
    stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"
  >
    <path v-for="d in paths" :key="d" :d="d" />
  </svg>
</template>

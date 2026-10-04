<!-- Copyright (c) 2026 Mancala Gaming. All rights reserved. -->
<script setup>
/** Auto / Light / Dark, remembered per machine. */
import { NTabs, NTab } from 'naive-ui';
import { setThemeMode, themeMode } from '../lib/theme.js';

const emit = defineEmits(['change']);

function pick(mode) {
  setThemeMode(mode);
  emit('change', mode);
}
</script>

<template>
  <NTabs
    type="segment"
    size="small"
    :value="themeMode"
    class="theme-switch"
    @update:value="pick"
  >
    <NTab name="system" tab="Auto" />
    <NTab name="light" tab="Light" />
    <NTab name="dark" tab="Dark" />
  </NTabs>
</template>

<style scoped>
.theme-switch {
  width: 210px;
}
</style>

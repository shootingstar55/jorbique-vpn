<script setup>
import { NConfigProvider, NDialogProvider, NMessageProvider, dateRuRU, ruRU } from 'naive-ui';
import AppShell from './components/AppShell.vue';
import RouteRulesPanel from './components/RouteRulesPanel.vue';
import MainPanel from './components/MainPanel.vue';
import UuidModal from './components/UuidModal.vue';
import LogsModal from './components/LogsModal.vue';
import ActivationScreen from './components/ActivationScreen.vue';
import { watchEffect } from 'vue';
import { isDark, naiveTheme, naiveThemeOverrides } from './lib/theme.js';
import { reportTheme } from './lib/android.js';

const search = new URLSearchParams(window.location.search);
const standaloneVpnConfig = search.has('set-uuid');
const standaloneRouteRules = search.has('route-rules');
const standaloneMain = search.has('main');
const standaloneActivate = search.has('activate');
const standaloneLogs = search.has('logs');

// The stylesheet paints the window background and the panels' own surfaces, and
// neither is a Naive component — so the choice is stamped on <html> for CSS to
// follow.
watchEffect(() => {
  document.documentElement.dataset.theme = isDark.value ? 'dark' : 'light';
  reportTheme(isDark.value);
});
</script>

<template>
  <!-- The providers have to sit above anything calling useMessage()/useDialog(), which is why
       the shell is a child rather than this component doing the work itself. -->
  <NConfigProvider :locale="ruRU" :date-locale="dateRuRU" :theme="naiveTheme" :theme-overrides="naiveThemeOverrides">
    <NMessageProvider :max="3" placement="bottom">
      <NDialogProvider>
        <ActivationScreen v-if="standaloneActivate" standalone />
        <UuidModal v-else-if="standaloneVpnConfig" :visible="true" standalone />
        <RouteRulesPanel v-else-if="standaloneRouteRules" />
        <MainPanel v-else-if="standaloneMain" />
        <LogsModal v-else-if="standaloneLogs" :visible="true" standalone />
        <AppShell v-else />
      </NDialogProvider>
    </NMessageProvider>
  </NConfigProvider>
</template>

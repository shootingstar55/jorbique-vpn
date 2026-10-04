<script setup>
import { NConfigProvider, NDialogProvider, NMessageProvider } from 'naive-ui';
import AppShell from './components/AppShell.vue';
import DashboardView from './components/DashboardView.vue';
import RouteRulesPanel from './components/RouteRulesPanel.vue';
import MainPanel from './components/MainPanel.vue';
import VpnConfigModal from './components/VpnConfigModal.vue';
import ActivationScreen from './components/ActivationScreen.vue';
import { watchEffect } from 'vue';
import { isDark, naiveTheme, naiveThemeOverrides } from './lib/theme.js';

const search = new URLSearchParams(window.location.search);
const standaloneDashboard = search.has('dashboard');
const standaloneVpnConfig = search.has('vpn-config');
const standaloneRouteRules = search.has('route-rules');
const standaloneMain = search.has('main');
const standaloneActivate = search.has('activate');

// The stylesheet paints the window background and the panels' own surfaces, and
// neither is a Naive component — so the choice is stamped on <html> for CSS to
// follow.
watchEffect(() => {
  document.documentElement.dataset.theme = isDark.value ? 'dark' : 'light';
});
</script>

<template>
  <!-- The providers have to sit above anything calling useMessage()/useDialog(), which is why
       the shell is a child rather than this component doing the work itself. -->
  <NConfigProvider :theme="naiveTheme" :theme-overrides="naiveThemeOverrides">
    <NMessageProvider :max="3" placement="bottom">
      <NDialogProvider>
        <ActivationScreen v-if="standaloneActivate" standalone />
        <VpnConfigModal v-else-if="standaloneVpnConfig" :visible="true" standalone />
        <DashboardView v-else-if="standaloneDashboard" standalone />
        <RouteRulesPanel v-else-if="standaloneRouteRules" />
        <MainPanel v-else-if="standaloneMain" />
        <AppShell v-else />
      </NDialogProvider>
    </NMessageProvider>
  </NConfigProvider>
</template>

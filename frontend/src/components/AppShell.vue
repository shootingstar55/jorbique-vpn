<script setup>
import { onMounted, ref } from 'vue';
import { useMessage } from 'naive-ui';
import { binding } from '../lib/bindings.js';
import ActivationScreen from './ActivationScreen.vue';
import MainScreen from './MainScreen.vue';
import DashboardModal from './DashboardModal.vue';
import IgnoreAppsModal from './IgnoreAppsModal.vue';
import LogsModal from './LogsModal.vue';
import UuidModal from './UuidModal.vue';

/**
 * Wires the screens together.
 *
 * Each screen owns its own state and talks to the backend itself; what has to cross between
 * them lives here — the toast, and the two things a modal asks of the main screen once it is
 * done: carry on with a connect that was waiting for a config, and reconnect so sing-box
 * picks up a changed ignore list.
 */
const message = useMessage();

/**
 * Whether a user UUID is stored: null until the backend has answered, false
 * while the first-run screen stands in for the whole app. Nothing below it
 * mounts before, so the sing-box download and the status poll wait for it too.
 */
const activated = ref(null);
onMounted(async () => {
  const hasUserUUID = binding('HasUserUUID');
  try {
    // Outside the app window (a dev server) there is no backend to ask.
    activated.value = hasUserUUID ? !!(await hasUserUUID()) : true;
  } catch {
    // An unanswered check asks for the UUID again rather than opening the app.
    activated.value = false;
  }
});

const mainScreen = ref(null);

const vpnConfigOpen = ref(false);
/** Set when the config modal was opened from a connect attempt that stalled without one. */
const vpnConfigResumesConnect = ref(false);
const ignoreAppsOpen = ref(false);
const logsOpen = ref(false);
const dashboardOpen = ref(false);
let mainWindowSize = null;

function openVpnConfig(options = {}) {
  vpnConfigResumesConnect.value = !!options.resumeConnect;
  vpnConfigOpen.value = true;
}

function onVpnConfigSaved({ resumeConnect }) {
  if (resumeConnect) mainScreen.value?.resumeConnect();
  else mainScreen.value?.reconnectIfConnected();
}

function onIgnoreListSaved() {
  // sing-box only picks up a changed ignore list on reconnect.
  mainScreen.value?.reconnectIfConnected();
}

async function openDashboard() {
  const runtime = window.runtime;
  try {
    mainWindowSize = await runtime?.WindowGetSize?.();
    runtime?.WindowSetSize?.(1040, 720);
    runtime?.WindowCenter?.();
  } catch {
    // Dashboard data still works when the host cannot resize this window.
  }
  dashboardOpen.value = true;
}

function setDashboardOpen(visible) {
  dashboardOpen.value = visible;
  if (!visible && mainWindowSize) {
    window.runtime?.WindowSetSize?.(mainWindowSize.w, mainWindowSize.h);
    window.runtime?.WindowCenter?.();
    mainWindowSize = null;
  }
}
</script>

<template>
  <div class="shell">
    <ActivationScreen v-if="activated === false" @activated="activated = true" />
    <template v-else-if="activated">
      <MainScreen
        ref="mainScreen"
        @error="message.error($event)"
        @toast="message.success($event)"
        @open-vpn-config="openVpnConfig"
        @open-ignore-apps="ignoreAppsOpen = true"
        @open-logs="logsOpen = true"
        @open-dashboard="openDashboard"
      />

      <UuidModal
        v-model:visible="vpnConfigOpen"
        :resume-connect="vpnConfigResumesConnect"
        @error="message.error($event)"
        @toast="message.success($event)"
        @saved="onVpnConfigSaved"
      />

      <IgnoreAppsModal
        v-model:visible="ignoreAppsOpen"
        @error="message.error($event)"
        @toast="message.success($event)"
        @saved="onIgnoreListSaved"
      />

      <LogsModal v-model:visible="logsOpen" @error="message.error($event)" @toast="message.success($event)" />
      <DashboardModal :visible="dashboardOpen" @update:visible="setDashboardOpen" />
    </template>
  </div>
</template>

<style scoped>
.shell {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  width: 100%;
  box-sizing: border-box;
  padding: 8px 12px;
  overflow-y: auto;
}

@media (min-width: 640px) {
  .shell {
    padding: 10px 16px;
  }
}
</style>

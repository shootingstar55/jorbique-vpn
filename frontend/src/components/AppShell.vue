<script setup>
import { onMounted, onUnmounted, ref } from 'vue';
import { useMessage } from 'naive-ui';
import { binding } from '../lib/bindings.js';
import { getErrMsg } from '../lib/errors.js';
import { explainActivationError, looksLikeUUID } from '../lib/activation.js';
import ActivationScreen from './ActivationScreen.vue';
import MainScreen from './MainScreen.vue';
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
  takeImportLink();
  window.addEventListener('jorbique-import', takeImportLink);
  if (window.runtime && typeof window.runtime.EventsOn === 'function') {
    offImportEvent = window.runtime.EventsOn('jorbique-import', takeImportLink);
  }
});
onUnmounted(() => {
  window.removeEventListener('jorbique-import', takeImportLink);
  if (typeof offImportEvent === 'function') offImportEvent();
});

/**
 * «Открыть в Jorbique VPN» on the /sub page opens the app with a
 * jorbique://import?uuid=… link. The native side keeps the UUID until the page
 * takes it — on load, and on the 'jorbique-import' event when the app was
 * already open. On the first-run screen it fills the field and submits it; in
 * the app it replaces the stored UUID the way the UUID window would.
 */
let offImportEvent = null;
const importUuid = ref('');
let importing = false;
async function takeImportLink() {
  const take = binding('TakePendingImportUUID');
  if (!take || importing || activated.value === null) return;
  importing = true;
  try {
    const uuid = String((await take()) || '').trim().toLowerCase();
    if (!looksLikeUUID(uuid)) return;
    if (!activated.value) {
      importUuid.value = uuid;
      return;
    }
    const current = binding('GetUserUUID');
    if (current && String((await current()) || '').trim().toLowerCase() === uuid) {
      message.info('Этот UUID уже сохранён. Можно подключаться.');
      return;
    }
    await binding('SetUserUUID')(uuid);
    message.success('UUID из ссылки сохранён. Можно подключаться.');
    mainScreen.value?.reconnectIfConnected();
  } catch (e) {
    message.error(explainActivationError(getErrMsg(e, '')));
  } finally {
    importing = false;
  }
}

function onActivated() {
  activated.value = true;
  importUuid.value = '';
}

const mainScreen = ref(null);

const vpnConfigOpen = ref(false);
/** Set when the config modal was opened from a connect attempt that stalled without one. */
const vpnConfigResumesConnect = ref(false);
const ignoreAppsOpen = ref(false);
const logsOpen = ref(false);

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
</script>

<template>
  <div class="shell">
    <ActivationScreen v-if="activated === false" :preset-uuid="importUuid" @activated="onActivated" />
    <template v-else-if="activated">
      <MainScreen
        ref="mainScreen"
        @error="message.error($event)"
        @toast="message.success($event)"
        @open-vpn-config="openVpnConfig"
        @open-ignore-apps="ignoreAppsOpen = true"
        @open-logs="logsOpen = true"
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

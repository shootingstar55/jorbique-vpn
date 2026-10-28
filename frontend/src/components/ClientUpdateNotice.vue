<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { NButton, NModal, NProgress, useDialog, useMessage } from 'naive-ui';
import appVersion from '../../../VERSION?raw';
import { binding } from '../lib/bindings.js';
import { panelAPI } from '../lib/panel-api.js';
import { getErrMsg } from '../lib/errors.js';

const status = ref({ currentVersion: appVersion.trim(), version: '', available: false });
const checking = ref(false);
const installing = ref(false);
const restarting = ref(false);
const progress = ref({ stage: 'checking', downloaded: 0, total: 0 });
const percent = computed(() => progress.value.total > 0 ? Math.min(100, Math.floor(progress.value.downloaded / progress.value.total * 100)) : 0);
const progressLabel = computed(() => restarting.value ? 'Перезапускаем клиент…' : ({
  checking: 'Проверяем новую версию…', downloading: `Скачиваем обновление — ${percent.value}%`,
  verifying: 'Проверяем целостность пакета…', disconnecting: 'Отключаем VPN…',
  preparing: 'Готовим установку…', ready: 'Открываем установщик…',
}[progress.value.stage] || 'Готовим обновление…'));
const message = useMessage();
const dialog = useDialog();
let timer;
let progressTimer;
let progressInFlight = false;

function call(action) {
  const native = binding({ check: 'CheckClientUpdate', install: 'InstallClientUpdate', progress: 'GetClientUpdateProgress' }[action]);
  return native ? native() : panelAPI.ask('client-update', { action });
}

async function readProgress() {
  if (progressInFlight || !installing.value || restarting.value) return;
  progressInFlight = true;
  try {
    const value = await call('progress');
    if (installing.value && !restarting.value && value?.stage) progress.value = value;
  } catch { /* A missed progress sample must not cancel a verified installation. */ }
  finally { progressInFlight = false; }
}

async function check() {
  if (checking.value || installing.value) return;
  checking.value = true;
  try {
    status.value = await call('check');
  } catch {
    status.value = { ...status.value, version: '', available: false };
  } finally {
    checking.value = false;
  }
}

async function install() {
  if (installing.value) return;
  installing.value = true;
  restarting.value = false;
  progress.value = { stage: 'checking', downloaded: 0, total: 0 };
  progressTimer = setInterval(readProgress, 250);
  try {
    restarting.value = (await call('install')) === true;
    if (!restarting.value) message.success('Обновление проверено. Следуйте указаниям системы.');
  } catch (e) {
    message.error(getErrMsg(e, 'Не удалось обновить клиент'));
  } finally {
    clearInterval(progressTimer);
    if (!restarting.value) installing.value = false;
  }
}

function confirmInstall() {
  dialog.warning({
    title: `Обновить клиент до ${status.value?.version}?`,
    content: 'Приложение скачает и проверит обновление. На Windows VPN отключится, а клиент перезапустится. На macOS и Android откроется системный установщик.',
    positiveText: 'Обновить', negativeText: 'Позже',
    onPositiveClick: install,
  });
}

onMounted(() => {
  check();
  timer = setInterval(check, 6 * 60 * 60 * 1000);
});
onBeforeUnmount(() => { clearInterval(timer); clearInterval(progressTimer); });
</script>

<template>
  <div class="client-update">
    <NModal :show="installing" preset="card" title="Обновление клиента" :closable="false" :mask-closable="false" :close-on-esc="false" class="dialog" style="max-width: 420px">
      <div role="status" aria-live="polite">
        <p>{{ progressLabel }}</p>
        <NProgress type="line" :percentage="percent" :show-indicator="false" :processing="true" :color="'#5ff0ae'" />
        <p v-if="progress.total" class="client-update__detail">{{ (progress.downloaded / 1048576).toFixed(1) }} из {{ (progress.total / 1048576).toFixed(1) }} МБ</p>
        <p class="client-update__detail">{{ restarting ? 'Окно закроется и откроется автоматически. Подготовка запуска может занять несколько секунд.' : 'Дождитесь завершения. VPN отключится только после проверки пакета.' }}</p>
      </div>
    </NModal>
    <div class="client-update__versions" aria-live="polite" :aria-busy="checking">
      <span>Текущая версия <b>{{ status.currentVersion }}</b></span>
      <span>Последняя версия <b>{{ checking ? 'Проверяем…' : (status.version || 'Не удалось узнать') }}</b></span>
    </div>
    <div v-if="status.available" class="client-update__actions">
      <NButton class="client-update__install" type="primary" color="#5ff0ae" text-color="#061423" :loading="installing" :disabled="checking" @click="confirmInstall">
        {{ installing ? 'Скачиваем и проверяем…' : 'Обновиться' }}
      </NButton>
    </div>
  </div>
</template>

<style scoped>
.client-update { width: 100%; text-align: center; }
.client-update__versions { display: flex; flex-wrap: wrap; justify-content: center; gap: 4px 16px; color: var(--text-dim); font-size: 12px; }
.client-update__versions b { color: var(--text); font-weight: 600; }
.client-update__actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: center; gap: 8px; margin-top: 4px; }
.client-update__install { min-width: 160px; font-weight: 700; box-shadow: 0 4px 20px rgba(95, 240, 174, .35); }
.client-update__detail { color: var(--text-dim); font-size: 13px; line-height: 1.5; }
</style>

<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue';
import { NButton, useDialog, useMessage } from 'naive-ui';
import appVersion from '../../../VERSION?raw';
import { binding } from '../lib/bindings.js';
import { panelAPI } from '../lib/panel-api.js';
import { getErrMsg } from '../lib/errors.js';

const status = ref({ currentVersion: appVersion.trim(), version: '', available: false });
const checking = ref(false);
const installing = ref(false);
const message = useMessage();
const dialog = useDialog();
let timer;

function call(action) {
  const native = binding(action === 'check' ? 'CheckClientUpdate' : 'InstallClientUpdate');
  return native ? native() : panelAPI.ask('client-update', { action });
}

async function check(manual = false) {
  if (checking.value || installing.value) return;
  checking.value = true;
  try {
    status.value = await call('check');
    if (manual && status.value?.version && !status.value.available) message.info('Установлена актуальная версия клиента');
  } catch (e) {
    status.value = { ...status.value, version: '', available: false };
    if (manual) message.error(getErrMsg(e, 'Не удалось проверить обновления'));
  } finally {
    checking.value = false;
  }
}

async function install() {
  if (installing.value) return;
  installing.value = true;
  try {
    await call('install');
    message.success('Обновление подготовлено. Следуйте указаниям системы.');
  } catch (e) {
    message.error(getErrMsg(e, 'Не удалось обновить клиент'));
  } finally {
    installing.value = false;
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
onBeforeUnmount(() => clearInterval(timer));
</script>

<template>
  <div class="client-update">
    <div class="client-update__versions" aria-live="polite" :aria-busy="checking">
      <span>Текущая версия <b>{{ status.currentVersion }}</b></span>
      <span>Последняя версия <b>{{ checking ? 'Проверяем…' : (status.version || 'Не удалось узнать') }}</b></span>
    </div>
    <div class="client-update__actions">
      <NButton quaternary size="small" :loading="checking" :disabled="installing" @click="check(true)">
        Проверить обновления
      </NButton>
      <NButton v-if="status.available" type="primary" size="small" :loading="installing" :disabled="checking" @click="confirmInstall">
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
</style>

<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue';
import { NAlert, NButton, useDialog, useMessage } from 'naive-ui';
import { binding } from '../lib/bindings.js';
import { panelAPI } from '../lib/panel-api.js';
import { getErrMsg } from '../lib/errors.js';

const status = ref(null);
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
    if (manual && !status.value?.available) message.info('Установлена актуальная версия клиента');
  } catch (e) {
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
    <NAlert v-if="status?.available" type="info" :bordered="false" :show-icon="false">
      <div class="client-update__offer">
        <span>Доступна версия {{ status.version }}</span>
        <NButton type="primary" size="small" :loading="installing" :disabled="checking" @click="confirmInstall">
          {{ installing ? 'Скачиваем и проверяем…' : 'Обновить' }}
        </NButton>
      </div>
    </NAlert>
    <NButton quaternary size="tiny" :loading="checking" :disabled="installing" @click="check(true)">
      <span v-if="status?.currentVersion">v{{ status.currentVersion }} · </span>Проверить обновления
    </NButton>
  </div>
</template>

<style scoped>
.client-update { width: 100%; text-align: center; }
.client-update__offer { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 8px; }
.client-update > .n-button { margin-top: 4px; color: var(--text-dim); }
</style>

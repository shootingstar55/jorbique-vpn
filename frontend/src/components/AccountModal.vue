<script setup>
/**
 * «Личный кабинет»: who the UUID belongs to, until when it works, and the devices bound
 * to it under the limit the server sets. A device is its name and OS, as the metrics
 * report it; unbinding frees a slot, and only «Привязать это устройство» brings an
 * unbound device back.
 */
import { computed, ref, watch } from 'vue';
import { NAlert, NButton, NIcon, NModal, NSpin, NTag, useDialog } from 'naive-ui';
import AppIcon from './AppIcon.vue';
import { accountAPI, formatDate } from '../lib/account-api.js';
import { copyText } from '../lib/logs-api.js';
import { getErrMsg } from '../lib/errors.js';
import { labels } from '../lib/labels.js';

const props = defineProps({
  visible: { type: Boolean, default: false },
});
const emit = defineEmits(['error', 'toast', 'update:visible']);
const dialog = useDialog();

const profile = ref(null);
const loading = ref(false);
const acting = ref('');
const error = ref('');
let epoch = 0;

const devices = computed(() => profile.value?.devices || []);
const limit = computed(() => profile.value?.devices_limit ?? null);
const current = computed(() => profile.value?.current_device || {});
const full = computed(() => limit.value !== null && devices.value.length >= limit.value);
const countText = computed(() => (limit.value === null
  ? `${devices.value.length}, без ограничения`
  : `${devices.value.length} из ${limit.value}`));
const expiresText = computed(() => (profile.value?.expires ? `до ${formatDate(profile.value.expires)}` : 'бессрочно'));

async function load() {
  const mine = ++epoch;
  loading.value = true;
  error.value = '';
  try {
    const next = await accountAPI.get();
    if (mine === epoch) profile.value = next;
  } catch (e) {
    if (mine === epoch) error.value = getErrMsg(e, 'Не удалось открыть личный кабинет');
  } finally {
    if (mine === epoch) loading.value = false;
  }
}

watch(() => props.visible, (open) => {
  if (open) load();
}, { immediate: true });

async function run(action, task, done) {
  if (acting.value) return;
  acting.value = action;
  try {
    profile.value = await task();
    emit('toast', done);
  } catch (e) {
    emit('error', getErrMsg(e, 'Действие не выполнено'));
    await load();
  } finally {
    acting.value = '';
  }
}

/** Opening the screen already checks this device in: a new one is bound there while a slot is free. */
function bind() {
  run('bind', () => accountAPI.bindThisDevice(), 'Устройство привязано');
}

function unbind(device) {
  dialog.warning({
    title: 'Отвязать устройство',
    content: device.current
      ? 'Отвязать это устройство? Пока вы не привяжете его снова, VPN на нём не подключится.'
      : `Отвязать «${device.name}»? На нём VPN перестанет подключаться, а место освободится.`,
    positiveText: 'Отвязать',
    negativeText: labels.cancel,
    onPositiveClick: () => run(`unbind:${device.id}`, () => accountAPI.unbind(device.id), 'Устройство отвязано'),
  });
}

async function copyUUID() {
  try {
    await copyText(profile.value?.uuid || '');
    emit('toast', 'UUID скопирован');
  } catch {
    emit('error', 'Не удалось скопировать');
  }
}

function close() {
  emit('update:visible', false);
}
</script>

<template>
  <NModal
    :show="visible"
    preset="card"
    class="dialog"
    :bordered="false"
    title="Личный кабинет"
    @update:show="$event || close()"
  >
    <NSpin :show="loading && !profile">
      <NAlert v-if="error" type="error" :show-icon="false" class="block">{{ error }}</NAlert>

      <template v-if="profile">
        <dl class="facts">
          <div class="fact">
            <dt>Пользователь</dt>
            <dd>{{ profile.name }}</dd>
          </div>
          <div class="fact">
            <dt>UUID</dt>
            <dd class="fact__uuid">
              <span class="mono">{{ profile.uuid }}</span>
              <NButton quaternary size="tiny" aria-label="Копировать UUID" @click="copyUUID">
                <template #icon><NIcon><AppIcon name="copy" /></NIcon></template>
              </NButton>
            </dd>
          </div>
          <div class="fact">
            <dt>Подписка</dt>
            <dd>{{ expiresText }}</dd>
          </div>
        </dl>

        <div class="devices__head">
          <h3 class="devices__title">Устройства</h3>
          <span class="devices__count" :class="{ 'is-full': full }">{{ countText }}</span>
        </div>

        <NAlert v-if="current.revoked && !current.bound" type="warning" :show-icon="false" class="block">
          Это устройство отвязано, VPN на нём не подключится.
        </NAlert>
        <NAlert v-else-if="!current.bound && full" type="warning" :show-icon="false" class="block">
          Лимит устройств исчерпан. Отвяжите одно, чтобы подключить это.
        </NAlert>

        <ul v-if="devices.length" class="devices">
          <li v-for="device in devices" :key="device.id" class="device">
            <div class="device__text">
              <p class="device__name">
                {{ device.name }}
                <NTag v-if="device.current" size="small" round :bordered="false" type="success">это устройство</NTag>
              </p>
              <p class="device__meta">
                {{ device.os || 'ОС неизвестна' }} · привязано {{ formatDate(device.bound_at) }}
              </p>
            </div>
            <NButton
              quaternary
              size="small"
              type="error"
              :loading="acting === `unbind:${device.id}`"
              :disabled="!!acting"
              @click="unbind(device)"
            >
              Отвязать
            </NButton>
          </li>
        </ul>
        <p v-else class="devices__empty">Пока ни одного.</p>
      </template>
    </NSpin>

    <template #footer>
      <div class="footer">
        <NButton quaternary :disabled="loading" class="footer__refresh" @click="load">
          <template #icon><NIcon><AppIcon name="refresh" /></NIcon></template>
          Обновить
        </NButton>
        <NButton
          v-if="profile && !current.bound"
          type="primary"
          :loading="acting === 'bind'"
          :disabled="!!acting || full"
          @click="bind"
        >
          <template #icon><NIcon><AppIcon name="plus" /></NIcon></template>
          Привязать это устройство
        </NButton>
        <NButton quaternary @click="close">{{ labels.close }}</NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.block {
  margin-bottom: 12px;
}

.facts {
  margin: 0 0 16px;
  display: grid;
  gap: 8px;
}

.fact {
  display: grid;
  grid-template-columns: 110px 1fr;
  gap: 8px;
  align-items: center;
}

.fact dt {
  color: var(--text-muted);
  font-size: 13px;
}

.fact dd {
  margin: 0;
  color: var(--text);
  min-width: 0;
}

.fact__uuid {
  display: flex;
  align-items: center;
  gap: 4px;
}

.mono {
  font-family: var(--n-font-family-mono, monospace);
  font-size: 12px;
  overflow-wrap: anywhere;
  user-select: text;
}

.devices__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 8px;
}

.devices__title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}

.devices__count {
  color: var(--text-muted);
  font-size: 13px;
}

.devices__count.is-full {
  color: #d03050;
}

.devices {
  list-style: none;
  margin: 0;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 12px;
  overflow: hidden;
}

.device {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px;
  background: var(--surface-sunken);
}

.device + .device {
  border-top: 1px solid var(--border);
}

.device__text {
  min-width: 0;
}

.device__name {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  overflow-wrap: anywhere;
}

.device__meta {
  margin: 2px 0 0;
  color: var(--text-muted);
  font-size: 12px;
}

.devices__empty {
  margin: 0;
  color: var(--text-muted);
  font-size: 13px;
}

.footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}

.footer__refresh {
  margin-right: auto;
}

@media (max-width: 420px) {
  .fact {
    grid-template-columns: 1fr;
    gap: 2px;
  }
}
</style>

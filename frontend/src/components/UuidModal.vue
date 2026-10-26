<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { NAlert, NButton, NInput, NModal, useMessage } from 'naive-ui';
import { binding } from '../lib/bindings.js';
import { panelAPI } from '../lib/panel-api.js';
import { getErrMsg } from '../lib/errors.js';
import { looksLikeUUID } from '../lib/activation.js';

const props = defineProps({
  visible: { type: Boolean, default: false },
  standalone: { type: Boolean, default: false },
  resumeConnect: { type: Boolean, default: false },
});
const emit = defineEmits(['error', 'toast', 'saved', 'update:visible']);
const message = useMessage();
const uuid = ref('');
const hydrated = ref(false);
const saving = ref(false);
const error = ref('');
let epoch = 0;

async function load() {
  const current = ++epoch;
  hydrated.value = false;
  error.value = '';
  try {
    const get = binding('GetUserUUID');
    const value = get ? await get() : (await panelAPI.load('config'))?.uuid;
    if (current !== epoch) return;
    uuid.value = value || '';
    hydrated.value = true;
  } catch (e) {
    if (current === epoch) error.value = getErrMsg(e, 'Could not load your UUID. Reopen this window to retry.');
  }
}

async function close() {
  if (saving.value) return;
  ++epoch;
  hydrated.value = false;
  if (props.standalone) await panelAPI.close();
  else emit('update:visible', false);
}

async function save() {
  if (!hydrated.value || saving.value) return;
  const value = uuid.value.trim();
  if (!looksLikeUUID(value)) { error.value = 'Enter a valid user UUID'; return; }
  saving.value = true;
  error.value = '';
  try {
    const set = binding('SetUserUUID');
    if (set) await set(value);
    else await panelAPI.save('config', { uuid: value });
    if (props.standalone) message.success('UUID saved');
    else emit('toast', 'UUID saved');
    emit('saved', { resumeConnect: props.resumeConnect });
    saving.value = false;
    await close();
  } catch (e) {
    error.value = getErrMsg(e, 'Could not save your UUID');
  } finally {
    saving.value = false;
  }
}

watch(() => props.visible, (visible) => { if (visible) void load(); else { ++epoch; hydrated.value = false; } });
onMounted(() => { if (props.visible) void load(); });
onBeforeUnmount(() => { ++epoch; });
</script>

<template>
  <NModal
    :show="visible" preset="card" title="Set UUID" class="dialog dialog--uuid"
    :bordered="false" :mask-closable="!saving" :close-on-esc="!saving" :closable="!saving"
    @update:show="(show) => { if (!show) close(); }"
  >
    <form @submit.prevent="save">
      <p>Enter your user UUID. The best connection is chosen automatically.</p>
      <NInput
        v-model:value="uuid" placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
        :disabled="!hydrated || saving" :status="error ? 'error' : undefined"
        :input-props="{ spellcheck: 'false', autocomplete: 'off', 'aria-label': 'User UUID' }"
        autofocus
      />
      <NAlert v-if="error" type="error" :bordered="false" role="alert">{{ error }}</NAlert>
      <div class="footer">
        <NButton quaternary :disabled="saving" @click="close">Cancel</NButton>
        <NButton type="primary" attr-type="submit" :disabled="!hydrated || !uuid.trim()" :loading="saving">Save UUID</NButton>
      </div>
    </form>
  </NModal>
</template>

<style scoped>
p { margin: 0 0 14px; color: var(--text-muted); }
.n-alert { margin-top: 12px; }
.footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
</style>

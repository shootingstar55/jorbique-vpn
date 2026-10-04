<script setup>
/** The macOS window around the stack cards: load what is in force, save the pick, close. */
import { onMounted, ref } from 'vue';
import { NAlert } from 'naive-ui';
import StackPicker from './StackPicker.vue';
import { panelAPI } from '../lib/panel-api.js';
import { getErrMsg } from '../lib/errors.js';

const current = ref('');
const error = ref('');
const saving = ref(false);

onMounted(async () => {
  try {
    current.value = (await panelAPI.load('stack'))?.stack || '';
  } catch {
    current.value = '';
  }
});

async function pick(mode) {
  if (saving.value) return;
  saving.value = true;
  try {
    await panelAPI.save('stack', { stack: mode });
    await panelAPI.close();
  } catch (e) {
    error.value = getErrMsg(e, 'Could not change the stack');
    saving.value = false;
  }
}
</script>

<template>
  <div class="panel">
    <header class="panel__head">
      <h1>Select TUN mode</h1>
      <p>How sing-box runs its tunnel. Picking one applies it and reconnects.</p>
    </header>
    <NAlert v-if="error" type="error" :bordered="false" class="panel__error">{{ error }}</NAlert>
    <StackPicker :current="current" @pick="pick" />
  </div>
</template>

<style scoped>
.panel {
  padding: 22px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.panel__head h1 {
  margin: 0;
  font-size: 17px;
  color: var(--text);
}

.panel__head p {
  margin: 6px 0 0;
  font-size: 13px;
  color: var(--text-muted);
}

.panel__error {
  margin-bottom: 4px;
}
</style>

<script setup>
import { ref, watch } from 'vue';
import { NButton, NIcon, NModal, NScrollbar, NSelect, NSpin, useDialog } from 'naive-ui';
import AppIcon from './AppIcon.vue';
import { binding } from '../lib/bindings.js';
import { getErrMsg } from '../lib/errors.js';
import { labels } from '../lib/labels.js';

const props = defineProps({
  visible: { type: Boolean, default: false },
});

const emit = defineEmits(['error', 'toast', 'update:visible']);

const dialog = useDialog();

const DEFAULT_LOG = 'controller.log';
const files = ref([{ label: DEFAULT_LOG, value: DEFAULT_LOG }]);
const selected = ref(DEFAULT_LOG);
const content = ref('');
const loading = ref(false);

async function loadFiles() {
  const list = binding('ListLogFiles');
  if (!list) return;
  try {
    const names = await list();
    if (Array.isArray(names) && names.length) {
      files.value = names.map((name) => ({ label: name, value: name }));
      // Keep the current choice if it still exists, so a refresh does not jump the viewer
      // back to the first file.
      if (!names.includes(selected.value)) selected.value = names[0];
    }
  } catch {
    // The default file stays listed, so the viewer still opens on it.
  }
}

async function loadContent() {
  const get = binding('GetLogContent');
  if (!get) return;
  loading.value = true;
  try {
    const text = await get(selected.value || DEFAULT_LOG);
    content.value = text || '(empty)';
  } catch (e) {
    content.value = `Error: ${getErrMsg(e, 'Failed to load')}`;
  } finally {
    loading.value = false;
  }
}

watch(selected, () => {
  if (props.visible) void loadContent();
});

watch(
  () => props.visible,
  async (visible) => {
    if (!visible) return;
    await loadFiles();
    await loadContent();
  },
);

async function copy() {
  if (!content.value) return;
  try {
    await navigator.clipboard.writeText(content.value);
    emit('toast', 'Copied to clipboard');
  } catch {
    emit('error', 'Failed to copy');
  }
}

function clear() {
  const clearLogs = binding('ClearLogs');
  if (!clearLogs) return;
  dialog.warning({
    title: 'Clear logs',
    content: 'Delete all log files? This cannot be undone.',
    positiveText: 'Clear',
    negativeText: 'Cancel',
    onPositiveClick: async () => {
      try {
        await clearLogs();
        await loadContent();
      } catch (e) {
        emit('error', getErrMsg(e, 'Failed to clear'));
      }
    },
  });
}

function close() {
  emit('update:visible', false);
}
</script>

<template>
  <NModal
    :show="visible"
    preset="card"
    class="dialog dialog--wide"
    :bordered="false"
    title="Logs"
    @update:show="$event || close()"
  >
    <template #header-extra>
      <div class="tools">
        <NSelect v-model:value="selected" :options="files" size="small" class="tools__select" />
        <NButton quaternary size="small" @click="copy">
          <template #icon><NIcon><AppIcon name="copy" /></NIcon></template>
          {{ labels.copy }}
        </NButton>
        <NButton quaternary size="small" type="error" @click="clear">
          <template #icon><NIcon><AppIcon name="trash" /></NIcon></template>
          {{ labels.clear }}
        </NButton>
      </div>
    </template>

    <NSpin :show="loading">
      <NScrollbar class="viewer" x-scrollable>
        <pre class="viewer__text">{{ content }}</pre>
      </NScrollbar>
    </NSpin>

    <template #footer>
      <div class="footer">
        <NButton quaternary @click="close">{{ labels.close }}</NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.tools {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tools__select {
  width: 190px;
}

.viewer {
  max-height: 56vh;
  border-radius: 12px;
  background: var(--surface-sunken);
  border: 1px solid var(--border);
}

.viewer__text {
  margin: 0;
  padding: 14px 16px;
  font-family: var(--n-font-family-mono, monospace);
  font-size: 12px;
  line-height: 1.6;
  color: var(--text-muted);
  white-space: pre-wrap;
  word-break: break-word;
  min-height: 260px;
  user-select: text;
}

.footer {
  display: flex;
  justify-content: flex-end;
}
</style>

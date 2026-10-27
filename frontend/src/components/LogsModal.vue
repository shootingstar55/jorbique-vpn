<script setup>
import { onMounted, ref, watch } from 'vue';
import { NButton, NIcon, NModal, NScrollbar, NSelect, NSpin, useDialog, useMessage } from 'naive-ui';
import AppIcon from './AppIcon.vue';
import { copyText, logsAPI } from '../lib/logs-api.js';
import { panelAPI } from '../lib/panel-api.js';
import { getErrMsg } from '../lib/errors.js';
import { labels } from '../lib/labels.js';

const props = defineProps({
  visible: { type: Boolean, default: false },
  /** The macOS Logs window: the page is the whole window, and closing it closes the window. */
  standalone: { type: Boolean, default: false },
});

const emit = defineEmits(['error', 'toast', 'update:visible']);

const dialog = useDialog();
const message = useMessage();

const DEFAULT_LOG = 'controller.log';
const files = ref([]);
const selected = ref('');
const content = ref('');
const loading = ref(false);
/** Bumped by every load, so an answer that arrives after a newer request is dropped. */
let epoch = 0;

function toast(text) {
  if (props.standalone) message.success(text);
  else emit('toast', text);
}

function fail(text) {
  if (props.standalone) message.error(text);
  else emit('error', text);
}

async function loadFiles() {
  let names = [];
  try {
    names = await logsAPI.files();
  } catch {
    // An empty list still opens the viewer; the default file is tried below.
  }
  if (!Array.isArray(names)) names = [];
  files.value = names.map((name) => ({ label: name, value: name }));
  // Keep the current choice if it still exists, so a refresh does not jump the viewer
  // back to the first file.
  if (!names.includes(selected.value)) {
    selected.value = names.includes(DEFAULT_LOG) ? DEFAULT_LOG : names[0] || '';
  }
}

async function loadContent() {
  const current = ++epoch;
  if (!selected.value) {
    content.value = '(пусто)';
    return;
  }
  loading.value = true;
  try {
    const text = await logsAPI.content(selected.value);
    if (current === epoch) content.value = text || '(пусто)';
  } catch (e) {
    if (current === epoch) content.value = `Ошибка: ${getErrMsg(e, 'Не удалось загрузить')}`;
  } finally {
    if (current === epoch) loading.value = false;
  }
}

async function open() {
  await loadFiles();
  await loadContent();
}

watch(selected, () => {
  if (props.visible) void loadContent();
});

watch(
  () => props.visible,
  (visible) => {
    if (visible) void open();
    else ++epoch;
  },
);

onMounted(() => {
  if (props.visible) void open();
});

async function copy() {
  if (!content.value) return;
  try {
    await copyText(content.value);
    toast('Скопировано в буфер обмена');
  } catch {
    fail('Не удалось скопировать');
  }
}

function clear() {
  dialog.warning({
    title: 'Очистить логи',
    content: 'Очистить все файлы логов? Это действие нельзя отменить.',
    positiveText: labels.clear,
    negativeText: labels.cancel,
    onPositiveClick: async () => {
      try {
        await logsAPI.clear();
      } catch (e) {
        fail(getErrMsg(e, 'Не удалось очистить'));
      }
      // Cleared or not, the list and the text are re-read: a log can go away with Clear.
      await open();
    },
  });
}

function close() {
  ++epoch;
  if (props.standalone) void panelAPI.close();
  else emit('update:visible', false);
}
</script>

<template>
  <NModal
    :show="visible"
    preset="card"
    class="dialog dialog--wide"
    :bordered="false"
    :title="labels.logs"
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

@media (max-width: 560px) {
  .tools {
    width: 100%;
    gap: 4px;
  }

  .tools__select {
    flex: 1;
    width: auto;
    min-width: 0;
  }
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

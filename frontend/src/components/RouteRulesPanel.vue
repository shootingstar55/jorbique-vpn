<script setup>
/**
 * The route-rules window on macOS: which domains and which applications bypass
 * the VPN entirely.
 *
 * Adding an application needs the native file chooser, which a page cannot open
 * — the bridge does it and hands back the new list.
 */
import { onMounted, ref } from 'vue';
import { NAlert, NButton, NInput } from 'naive-ui';
import { panelAPI } from '../lib/panel-api.js';
import { getErrMsg } from '../lib/errors.js';
import { labels } from '../lib/labels.js';

const direct = ref('');
const processes = ref('');
/** @type {import('vue').Ref<{id: string, name: string, paths: string[]}[]>} */
const apps = ref([]);

const error = ref('');
const saving = ref(false);
const picking = ref(false);

const FIELDS = [
  { key: 'direct', label: 'Сайты вне VPN', hint: 'Адреса сайтов через запятую.' },
  { key: 'processes', label: 'Приложения вне VPN (по имени)', hint: 'Имена приложений через запятую.' },
];
const models = { direct, processes };

onMounted(async () => {
  try {
    const state = (await panelAPI.load('route-rules')) || {};
    direct.value = state.direct || '';
    processes.value = state.processes || '';
    apps.value = state.apps || [];
  } catch (e) {
    error.value = getErrMsg(e, 'Не удалось прочитать текущие правила');
  }
});

async function addApplications() {
  if (picking.value) return;
  picking.value = true;
  try {
    const result = await panelAPI.save('pick-apps', apps.value.flatMap((app) => app.paths));
    if (Array.isArray(result?.apps)) apps.value = result.apps;
  } catch (e) {
    error.value = getErrMsg(e, 'Не удалось добавить приложение');
  } finally {
    picking.value = false;
  }
}

function removeApp(id) {
  apps.value = apps.value.filter((app) => app.id !== id);
}

async function save() {
  if (saving.value) return;
  saving.value = true;
  error.value = '';
  try {
    await panelAPI.save('route-rules', {
      direct: direct.value,
      processes: processes.value,
      paths: apps.value.flatMap((app) => app.paths),
    });
    await panelAPI.close();
  } catch (e) {
    error.value = getErrMsg(e, 'Не удалось сохранить правила');
    saving.value = false;
  }
}
</script>

<template>
  <div class="panel">
    <header class="panel__head">
      <h1>Исключения</h1>
      <p>Выберите приложения и сайты, которые будут работать через обычный интернет, без VPN.</p>
    </header>

    <NAlert v-if="error" type="error" :bordered="false">{{ error }}</NAlert>

    <div v-for="field in FIELDS" :key="field.key" class="field">
      <label class="field__label" :for="`route-${field.key}`">{{ field.label }}</label>
      <NInput v-model:value="models[field.key].value" type="textarea" :rows="2"
              :placeholder="field.hint" :input-props="{ id: `route-${field.key}`, spellcheck: 'false' }" />
    </div>

    <section class="apps">
      <div class="apps__head">
        <span class="field__label">Приложения в обход VPN</span>
        <NButton size="small" tertiary :loading="picking" @click="addApplications">Добавить приложения…</NButton>
      </div>
      <p v-if="!apps.length" class="apps__empty">
        Пока пусто. Нажмите «Добавить приложения» и выберите .app.
      </p>
      <ul v-else class="apps__list">
        <li v-for="app in apps" :key="app.id">
          <span class="apps__name">{{ app.name }}</span>
          <span class="apps__count">исполняемых файлов: {{ app.paths.length }}</span>
          <NButton size="tiny" quaternary @click="removeApp(app.id)">✕</NButton>
        </li>
      </ul>
    </section>

    <div class="panel__footer">
      <NButton quaternary :disabled="saving" @click="panelAPI.close()">{{ labels.cancel }}</NButton>
      <NButton type="primary" :loading="saving" @click="save">Сохранить правила</NButton>
    </div>
  </div>
</template>

<style scoped>
.panel { padding: 22px; display: flex; flex-direction: column; gap: 14px; }
.panel__head h1 { margin: 0; font-size: 17px; color: var(--text); }
.panel__head p { margin: 6px 0 0; font-size: 13px; color: var(--text-muted); }
.field { display: flex; flex-direction: column; gap: 6px; }
.field__label { font-size: 13px; color: var(--text-muted); }
.apps__head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.apps__empty { margin: 8px 0 0; font-size: 13px; color: var(--text-dim); }
.apps__list { list-style: none; margin: 8px 0 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.apps__list li {
  display: flex; align-items: center; gap: 10px; padding: 8px 12px;
  border-radius: 10px; background: var(--surface); border: 1px solid var(--border);
}
.apps__name { color: var(--text); font-size: 13px; }
.apps__count { margin-left: auto; font-size: 12px; color: var(--text-dim); }
.panel__footer { display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px; }
</style>

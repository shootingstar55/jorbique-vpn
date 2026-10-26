<script setup>
import { computed, nextTick, ref, watch } from 'vue';
import { NButton, NCheckbox, NEmpty, NIcon, NInput, NModal, NScrollbar, NSpin, NTooltip } from 'naive-ui';
import AppIcon from './AppIcon.vue';
import { binding } from '../lib/bindings.js';
import { getErrMsg } from '../lib/errors.js';
import { labels } from '../lib/labels.js';
import { isAndroid } from '../lib/android.js';

const props = defineProps({
  visible: { type: Boolean, default: false },
});

const emit = defineEmits(['error', 'toast', 'saved', 'update:visible']);

/** Debounce for the checkbox autosave, matching the delay the vanilla modal used. */
const AUTOSAVE_DELAY_MS = 500;
const SAVE_FAILED = 'Не удалось сохранить исключения';

const filter = ref('');

/**
 * Domains that bypass the VPN. They live here rather than in the VPN config window: a
 * domain exception and a process exception are the same answer to the same question.
 */
const directDomains = ref('');
const domainsError = ref('');
/** @type {import('vue').Ref<Array<{
 * pid?: number, process_name?: string, exe_name?: string, path?: string, window_title?: string, icon?: string
 * }>>} */
const apps = ref([]);
/** @type {import('vue').Ref<string[]>} Full paths for sing-box process_path. */
const paths = ref([]);
/**
 * Normalised process name -> the raw name the backend stores.
 *
 * This map, not the checkboxes, is the selection. Reading it back off the DOM (as the vanilla
 * modal did) loses every ignored app that is not currently rendered — one filtered out of
 * view, or one whose process simply is not running right now — so a single autosave would
 * silently drop it from the saved list.
 *
 * @type {import('vue').Ref<Map<string, string>>}
 */
const selected = ref(new Map());

const loading = ref(false);
const refreshing = ref(false);
/**
 * Gates every write until the first load has finished, so an empty modal can never overwrite
 * the saved lists — e.g. if it is closed while ListActiveApps is still in flight.
 */
const hydrated = ref(false);
const domainsHydrated = ref(false);

let saveTimer = null;

const needle = computed(() => filter.value.trim().toLowerCase());

const filteredPaths = computed(() =>
  needle.value ? paths.value.filter((p) => String(p).toLowerCase().includes(needle.value)) : paths.value,
);

const filteredApps = computed(() => {
  if (!needle.value) return apps.value;
  return apps.value.filter((p) => {
    const name = rawName(p).toLowerCase();
    const title = (p.window_title || '').toLowerCase();
    return name.includes(needle.value) || title.includes(needle.value);
  });
});

const selectedCount = computed(() => selected.value.size + paths.value.length);

function rawName(process) {
  return process.process_name || process.exe_name || '';
}

function normalizeProcName(s) {
  return (s || '').trim().toLowerCase();
}

/**
 * On Android a row is an installed app: the name to show is its label, and the package name
 * (what is saved) goes underneath where Windows shows the window title.
 */
function displayName(process) {
  if (isAndroid) return process.window_title || rawName(process);
  return rawName(process).replace(/\.exe$/i, '');
}

function subtitle(process) {
  return isAndroid ? rawName(process) : process.window_title;
}

function isSelected(process) {
  return selected.value.has(normalizeProcName(rawName(process)));
}

function toggle(process, on) {
  const raw = rawName(process);
  const key = normalizeProcName(raw);
  if (!key) return;
  const next = new Map(selected.value);
  if (on) next.set(key, raw);
  else next.delete(key);
  selected.value = next;
}

// --- Icons -------------------------------------------------------------------------
// Process names and window titles come straight from the OS process list. Binding them as
// text, rather than interpolating into an innerHTML string as the vanilla renderer did,
// is what removes the escaping burden and the semgrep suppressions that came with it.

const ICON_ACCENT_COUNT = 5;

function iconName(process) {
  const n = normalizeProcName(rawName(process));
  if (/(chrome|msedge|edge|firefox|brave|opera)/.test(n)) return 'browser';
  if (/(discord|telegram|whatsapp|signal)/.test(n)) return 'chat';
  if (/(spotify|music)/.test(n)) return 'music';
  if (/(cursor|code|idea|studio|webstorm|pycharm)/.test(n)) return 'code';
  return 'app';
}

/** Deterministic accent from the name, so an app keeps its colour between refreshes. */
function iconAccent(process) {
  const n = normalizeProcName(rawName(process));
  let h = 0;
  for (let i = 0; i < n.length; i++) h = (h * 31 + n.charCodeAt(i)) >>> 0;
  return `app__icon--accent-${h % ICON_ACCENT_COUNT}`;
}

// --- Persistence -------------------------------------------------------------------

async function persist() {
  if (!hydrated.value) return;
  const save = binding('SetIgnoredProcessNamesAndPaths');
  if (!save) return;
  await save([...selected.value.values()], [...paths.value]);
}

function scheduleSave() {
  if (!hydrated.value) return;
  if (!binding('SetIgnoredProcessNamesAndPaths')) return;
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    saveTimer = null;
    persist().catch((e) => emit('error', getErrMsg(e, SAVE_FAILED)));
  }, AUTOSAVE_DELAY_MS);
}

/**
 * Writes any pending change immediately. Awaits nextTick first so a mutation made in this same
 * tick has already been picked up by the watcher below — otherwise the debounce it queues
 * would survive the clearTimeout here and fire a second, redundant write.
 */
async function flush() {
  if (!hydrated.value) return;
  await nextTick();
  clearTimeout(saveTimer);
  saveTimer = null;
  await persist();
}

// Any change to either list schedules the debounced write, so no mutation site has to
// remember to call the autosave itself.
watch(() => [[...selected.value.keys()], [...paths.value]], scheduleSave);

// --- Loading -----------------------------------------------------------------------

async function load({ isRefresh = false } = {}) {
  if (isRefresh) refreshing.value = true;
  else loading.value = true;
  hydrated.value = false;
  clearTimeout(saveTimer);
  saveTimer = null;

  const getPaths = binding('GetIgnoredProcessPaths');
  const getNames = binding('GetIgnoredProcessNames');
  const listApps = binding('ListActiveApps');

  let nextPaths;
  let names;
  try {
    [nextPaths, names] = await Promise.all([
      getPaths ? getPaths() : [],
      getNames ? getNames() : [],
    ]);
  } catch (e) {
    emit('error', getErrMsg(e, 'Не удалось прочитать сохранённые исключения'));
    loading.value = false;
    refreshing.value = false;
    return;
  }

  let nextApps = [];
  try {
    nextApps = listApps ? await listApps() : [];
  } catch (e) {
    nextApps = [];
    emit('error', getErrMsg(e, isRefresh ? 'Не удалось обновить список приложений' : 'Не удалось получить список запущенных приложений'));
  }

  paths.value = nextPaths || [];
  selected.value = new Map((names || []).map((n) => [normalizeProcName(n), n]).filter(([key]) => key));
  apps.value = nextApps;
  loading.value = false;
  refreshing.value = false;

  // Let the watcher observe the freshly loaded lists while writes are still gated, so the load
  // itself becomes the new baseline rather than a change worth persisting.
  await nextTick();
  hydrated.value = true;
}

async function loadDomains() {
  domainsHydrated.value = false;
  const get = binding('GetDirectDomains');
  try {
    directDomains.value = get ? (await get()) || '' : '';
    domainsHydrated.value = true;
  } catch (e) {
    domainsError.value = getErrMsg(e, 'Не удалось прочитать список прямых доменов');
    emit('error', domainsError.value);
  }
}

watch(
  () => props.visible,
  async (visible) => {
    if (!visible) return;
    filter.value = '';
    domainsError.value = '';
    await Promise.all([load(), loadDomains()]);
  },
);

// --- Actions -----------------------------------------------------------------------

async function addExecutable() {
  const pick = binding('PickExecutableForBypass');
  if (!pick) {
    emit('error', 'Выбор файла недоступен');
    return;
  }
  try {
    const picked = await pick();
    const value = String(picked || '').trim();
    if (!value) return;
    const key = value.toLowerCase();
    if (paths.value.some((x) => x.toLowerCase() === key)) {
      emit('toast', 'Уже есть в списке');
      return;
    }
    paths.value = [...paths.value, value];
    await flush();
  } catch (e) {
    emit('error', getErrMsg(e, 'Не удалось выбрать файл'));
  }
}

async function removePath(path) {
  paths.value = paths.value.filter((x) => x !== path);
  try {
    await flush();
  } catch (e) {
    emit('error', getErrMsg(e, SAVE_FAILED));
  }
}

async function close({ persistFirst = true } = {}) {
  if (persistFirst) {
    try {
      await flush();
    } catch (e) {
      // Closing still wins: a save that keeps failing must not trap the user in the window.
      emit('error', getErrMsg(e, SAVE_FAILED));
    }
  }
  hydrated.value = false;
  emit('update:visible', false);
}

async function save() {
  if (!hydrated.value || !domainsHydrated.value) return;
  if (!binding('SetIgnoredProcessNamesAndPaths')) {
    await close({ persistFirst: false });
    return;
  }
  try {
    await flush();

    const saveDomains = binding('SetDirectDomains');
    if (saveDomains) {
      try {
        await saveDomains(directDomains.value);
        domainsError.value = '';
      } catch (e) {
        // A bad domain keeps the window open with the field marked, rather than
        // closing on a save that did not happen.
        domainsError.value = getErrMsg(e, 'Неверный домен').replace(/^(Direct\s+domains|Прямые\s+домены):\s*/i, '');
        emit('error', domainsError.value);
        return;
      }
    }

    await close({ persistFirst: false });
    emit('toast', 'Исключения сохранены');
    // The host decides what a save means beyond persistence — today, reconnecting so sing-box
    // reloads its config with the new list.
    emit('saved');
  } catch (e) {
    emit('error', getErrMsg(e, SAVE_FAILED));
  }
}

/** NModal drives this on backdrop click and Esc; both should persist, like Cancel does. */
function onUpdateShow(value) {
  if (!value) void close();
}
</script>

<template>
  <NModal
    :show="visible"
    preset="card"
    class="dialog dialog--wide"
    :bordered="false"
    title="Исключения"
    :auto-focus="!isAndroid"
    @update:show="onUpdateShow"
  >
    <template #header-extra>
      <span v-if="selectedCount" class="count">выбрано: {{ selectedCount }}</span>
    </template>

    <div class="body">
      <section class="domains">
        <label class="domains__label" for="direct-domains">Домены без VPN</label>
        <p class="domains__hint">
          Один <strong>домен</strong> на строку (без регулярных выражений). Правило срабатывает для
          самого домена и его поддоменов (<code>domain_suffix</code>), и они идут мимо VPN. Встроенные
          правила для <code>.ru</code>, <code>.su</code> и национальных доменов (punycode) уже работают,
          добавлять их сюда не нужно.
        </p>
        <NInput
          v-model:value="directDomains"
          type="textarea"
          placeholder="example.com"
          :rows="3"
          :status="domainsError ? 'error' : undefined"
          :input-props="{ spellcheck: 'false', id: 'direct-domains' }"
        />
      </section>

      <p v-if="isAndroid" class="lede">
        Отмеченные приложения работают мимо VPN: система не пускает их трафик в туннель.
      </p>
      <p v-else class="lede">
        Приложения в обход VPN: <code>process_name</code> для запущенных приложений ниже или
        <code>process_path</code> для полного пути к .exe.
      </p>

      <div class="toolbar">
        <NInput v-model:value="filter" placeholder="Поиск по списку" clearable />
        <NButton v-if="!isAndroid" quaternary :disabled="loading || !hydrated" @click="addExecutable">
          <template #icon><NIcon><AppIcon name="plus" /></NIcon></template>
          Добавить .exe
        </NButton>
        <NButton quaternary :loading="refreshing" :disabled="loading" @click="load({ isRefresh: true })">
          <template #icon><NIcon><AppIcon name="refresh" /></NIcon></template>
          Обновить
        </NButton>
      </div>

      <section v-if="!isAndroid" class="group">
        <p class="group__label">По пути</p>
        <NSpin :show="loading">
          <div class="paths">
            <NEmpty
              v-if="!loading && !paths.length"
              size="small"
              description="Нет файлов, добавленных по пути. Нажмите «Добавить .exe», чтобы выбрать файл."
            />
            <NEmpty v-else-if="!loading && !filteredPaths.length"
                    size="small" description="Нет путей, подходящих под поиск." />
            <div v-for="path in filteredPaths" v-else :key="path" class="path">
              <NTooltip trigger="hover" placement="top-start">
                <template #trigger>
                  <code class="path__value">{{ path }}</code>
                </template>
                {{ path }}
              </NTooltip>
              <NButton
                quaternary
                circle
                size="small"
                :disabled="!hydrated"
                :aria-label="`Удалить ${path}`"
                @click="removePath(path)"
              >
                <template #icon><NIcon><AppIcon name="trash" /></NIcon></template>
              </NButton>
            </div>
          </div>
        </NSpin>
      </section>

      <section class="group group--grow">
        <p class="group__label">{{ isAndroid ? 'Установленные приложения' : 'Запущенные приложения' }}</p>
        <NSpin :show="loading" class="group__spin">
          <NScrollbar class="apps">
            <NEmpty v-if="!loading && !filteredApps.length" size="small" description="Приложения не найдены." />
            <div v-for="(process, i) in filteredApps" v-else :key="process.pid ?? process.process_name ?? i" class="app">
              <NCheckbox
                :checked="isSelected(process)"
                :disabled="!hydrated"
                :aria-label="displayName(process)"
                @update:checked="toggle(process, $event)"
              />
              <img v-if="process.icon" :src="process.icon" alt="" width="26" height="26"
                   class="app__icon app__icon--img" />
              <span
                v-else
                class="app__icon"
                :class="iconAccent(process)"
              >
                <NIcon :size="16"><AppIcon :name="iconName(process)" /></NIcon>
              </span>
              <span class="app__text">
                <span class="app__name">{{ displayName(process) }}</span>
                <span v-if="subtitle(process)" class="app__title">{{ subtitle(process) }}</span>
              </span>
            </div>
          </NScrollbar>
        </NSpin>
      </section>
    </div>


    <template #footer>
      <div class="footer">
        <NButton quaternary @click="close()">{{ labels.cancel }}</NButton>
        <NButton type="primary" :disabled="!hydrated || !domainsHydrated" @click="save">{{ labels.save }}</NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.domains {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 14px;
  padding-top: 14px;
  border-top: 1px solid rgba(148, 163, 184, 0.14);
}

.domains__label {
  font-size: 13px;
  font-weight: 600;
}

.domains__hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.55;
  color: var(--text-dim);
}

.count {
  font-size: 12px;
  color: var(--text-dim);
}

.body {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-height: 0;
}

.lede {
  margin: 0;
  font-size: 13px;
  line-height: 1.5;
  color: var(--text-muted);
}

.lede code,
.path__value {
  font-family: var(--n-font-family-mono, monospace);
  font-size: 0.92em;
  color: var(--text-muted);
}

.toolbar {
  display: grid;
  grid-template-columns: 1fr;
  gap: 8px;
}

@media (min-width: 560px) {
  .toolbar {
    grid-template-columns: 1fr auto auto;
  }
}

.group {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.group--grow {
  flex: 1;
}

.group__spin {
  min-height: 0;
  flex: 1;
}

.group__label {
  margin: 0 0 6px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--text-dim);
}

.paths {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 132px;
  overflow-y: auto;
}

.path {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px 8px 14px;
  border-radius: 12px;
  background: var(--surface-sunken);
  border: 1px solid var(--border);
}

.path__value {
  flex: 1;
  min-width: 0;
  word-break: break-all;
  line-height: 1.4;
  user-select: text;
}

.apps {
  max-height: 40vh;
}

.app {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 10px 12px;
  margin-bottom: 6px;
  border-radius: 12px;
  cursor: pointer;
  background: var(--surface-sunken);
  border: 1px solid var(--border);
  transition: background 0.16s ease, border-color 0.16s ease;
}

.app:hover {
  background: var(--surface);
  border-color: var(--border-hover);
}

.app__icon {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  flex-shrink: 0;
  border-radius: 8px;
  color: var(--accent);
  background: color-mix(in srgb, var(--accent, var(--text-dim)) 14%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent, var(--text-dim)) 26%, transparent);
}

.app__icon--accent-0 { --accent: #34d399; }
.app__icon--accent-1 { --accent: #38bdf8; }
.app__icon--accent-2 { --accent: #a78bfa; }
.app__icon--accent-3 { --accent: #fbbf24; }
.app__icon--accent-4 { --accent: #fb7185; }

.app__icon--img {
  object-fit: contain;
  background: var(--surface-sunken);
  border-color: var(--border-hover);
}

.app__text {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1;
}

.app__name {
  font-size: 14px;
  font-weight: 600;
  color: var(--text);
  word-break: break-word;
}

.app__title {
  font-size: 12px;
  color: var(--text-dim);
  word-break: break-word;
  line-height: 1.35;
}

.footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>

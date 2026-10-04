<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { NAlert, NButton, NInput, NModal, NSwitch, useMessage } from 'naive-ui';
import { binding } from '../lib/bindings.js';
import { getErrMsg } from '../lib/errors.js';
import { labels } from '../lib/labels.js';
import { loadAvailableOutbounds, outboundChecked, outboundLabel } from '../lib/outbounds.js';

const props = defineProps({
  visible: { type: Boolean, default: false },
  standalone: { type: Boolean, default: false },
  /** True when this was opened from a connect attempt that stalled for want of a config. */
  resumeConnect: { type: Boolean, default: false },
});

const emit = defineEmits(['error', 'toast', 'saved', 'update:visible']);
const message = useMessage();

/** Debounce for the draft autosave, matching the delay the vanilla modal used. */
const DRAFT_DELAY_MS = 450;
/** How long a field stays highlighted after a failed save. */
const FIELD_ERROR_MS = 2500;

const uuid = ref('');

/**
 * The outbound tags the downloaded sing-box profile offers. Filled in from the backend when
 * this window opens: the server decides which protocols exist, so a list written here would
 * show what was true when the client shipped — a protocol removed months ago, and none of the
 * ones added since.
 * @type {import('vue').Ref<string[]>}
 */
const availableOutbounds = ref([]);
/** @type {import('vue').Ref<Record<string, boolean>>} Answer per tag; survives a re-render. */
const outboundEnabled = ref({});
/**
 * The NaiveProxy mode: naive alone instead of the protocols below. A mode rather than one more
 * toggle, because naive is the heaviest of them and in the automatic selector it would run
 * beside every other one.
 */
const naiveMode = ref(false);

/** Gates draft autosave until GetVpnUserConfig has filled the form, so an empty UI cannot wipe saved settings. */
const hydrated = ref(false);

const uuidInput = ref(null);
/** @type {import('vue').Ref<'uuid'|null>} */
const errorField = ref(null);
let errorTimer = null;

let draftTimer = null;

const enabledCount = computed(() => availableOutbounds.value.filter((t) => outboundEnabled.value[t] !== false).length);

// --- Loading -----------------------------------------------------------------------

function normalizeVpnUserConfig(raw) {
  if (raw && typeof raw === 'object') {
    const o = raw;
    const en = o.outboundEnabled || o.OutboundEnabled || {};
    return {
      uuid: o.uuid || o.UUID || '',
      outboundEnabled: typeof en === 'object' && en !== null ? en : {},
      naiveMode: Boolean(o.naiveMode ?? o.NaiveMode),
    };
  }
  return { uuid: '', outboundEnabled: {}, naiveMode: false };
}

async function localRequest(path, body) {
  const response = await fetch(new URL(`api/${path}`, window.location.href), body === undefined
    ? { cache: 'no-store' }
    : {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
  if (!response.ok) {
    throw new Error((await response.text()).trim() || `Request failed (${response.status})`);
  }
  return response.status === 204 ? null : response.json();
}

function reportError(text) {
  if (props.standalone) message.error(text);
  else emit('error', text);
}

function reportToast(text) {
  if (props.standalone) message.success(text);
  else emit('toast', text);
}

/**
 * Folds the saved answers over what is on offer. An outbound nobody has answered for is on:
 * that is how a protocol the server adds starts working without anyone opening this window. A
 * tag the saved config does not mention keeps whatever it had here, so a reload never silently
 * flips a toggle the user set moments ago.
 */
function applyOutboundEnabled(stored) {
  const previous = outboundEnabled.value;
  /** @type {Record<string, boolean>} */
  const next = {};
  for (const tag of availableOutbounds.value) next[tag] = outboundChecked(stored, previous, tag);
  outboundEnabled.value = next;
}

/**
 * Bumped by every load and by close. A load that finds it moved on was overtaken — by a newer
 * load or by the modal closing — and drops its answer, so a slow earlier reply cannot overwrite
 * what the user already changed.
 */
let loadEpoch = 0;

/** Reports a failed load unless a newer load has already replaced it. */
function reportLoadError(epoch, e) {
  if (epoch === loadEpoch) reportError(getErrMsg(e, 'Could not load VPN configuration'));
}

async function load() {
  const epoch = ++loadEpoch;
  hydrated.value = false;
  clearTimeout(draftTimer);
  draftTimer = null;
  errorField.value = null;

  if (props.standalone) {
    try {
      const raw = await localRequest('config');
      if (epoch !== loadEpoch) return;
      const config = normalizeVpnUserConfig(raw);
      uuid.value = config.uuid;
      naiveMode.value = config.naiveMode;
      availableOutbounds.value = Array.isArray(raw?.availableOutbounds) ? raw.availableOutbounds : [];
      applyOutboundEnabled(config.outboundEnabled);
      hydrated.value = true;
    } catch (e) {
      reportLoadError(epoch, e);
    }
    return;
  }

  const get = binding('GetVpnUserConfig');
  if (!get) return;
  try {
    // Everything is read before anything is shown, so the one staleness check below covers it all.
    const [raw, tags] = await Promise.all([get(), loadAvailableOutbounds()]);
    if (epoch !== loadEpoch) return;
    const config = normalizeVpnUserConfig(raw);
    uuid.value = config.uuid;
    naiveMode.value = config.naiveMode;
    availableOutbounds.value = tags;
    applyOutboundEnabled(config.outboundEnabled);
  } catch (e) {
    reportLoadError(epoch, e);
    return;
  }
  hydrated.value = true;
}

// --- Draft autosave ----------------------------------------------------------------

function writeDraft() {
  if (props.standalone) return;
  const save = binding('SaveVpnFormDraft');
  if (!save) return;
  // Fire and forget: the draft is best effort, and Save is what writes the config.
  save(uuid.value, '', { ...outboundEnabled.value });
}

function scheduleDraftSave() {
  if (!hydrated.value) return;
  clearTimeout(draftTimer);
  draftTimer = setTimeout(() => {
    draftTimer = null;
    writeDraft();
  }, DRAFT_DELAY_MS);
}

function flushDraftSave() {
  if (!hydrated.value) return;
  clearTimeout(draftTimer);
  draftTimer = null;
  writeDraft();
}

// Every field the draft carries feeds the same debounce, so no input handler has to remember
// to call it — which is what the modal-wide 'input' listener was standing in for.
watch([uuid, () => ({ ...outboundEnabled.value })], scheduleDraftSave);

watch(
  () => props.visible,
  async (visible) => {
    if (visible) await load();
  },
  { immediate: true },
);

// A window being hidden is the one exit the modal never sees as a close, so the pending draft
// has to go out here or a minimise mid-edit loses it. flushDraftSave is gated on hydrated, so
// this is a no-op whenever the modal is not open and loaded.
function onVisibilityChange() {
  if (document.visibilityState === 'hidden') flushDraftSave();
}

onMounted(() => document.addEventListener('visibilitychange', onVisibilityChange));
onBeforeUnmount(() => document.removeEventListener('visibilitychange', onVisibilityChange));

// --- Saving ------------------------------------------------------------------------

function markFieldError(field, message) {
  errorField.value = field;
  const el = uuidInput.value;
  try {
    el?.focus();
  } catch {
    // A closing native window may reject focus; the error message still appears below.
  }
  if (message) reportError(message);
  // Reset the highlight window on each call, so repeated Save clicks do not let an early timer
  // clear the highlight the latest click just put up.
  clearTimeout(errorTimer);
  errorTimer = setTimeout(() => {
    errorField.value = null;
    errorTimer = null;
  }, FIELD_ERROR_MS);
}

function close({ persistDraft = true } = {}) {
  if (persistDraft && !props.standalone) flushDraftSave();
  loadEpoch++;
  hydrated.value = false;
  if (props.standalone) {
    localRequest('close', {}).catch((e) => reportError(getErrMsg(e, 'Could not close the window')));
    return;
  }
  emit('update:visible', false);
}

async function save() {
  if (!hydrated.value) return;
  try {
    if (props.standalone) {
      await localRequest('config', {
        uuid: uuid.value.trim(),
        outboundEnabled: { ...outboundEnabled.value },
        naiveMode: naiveMode.value,
      });
    } else {
      const set = binding('SetVpnUserConfig');
      if (!set) return;
      await set(uuid.value.trim(), { ...outboundEnabled.value });
      // After the account: a UUID the server refuses must not leave the mode switched.
      const setMode = binding('SetNaiveMode');
      if (setMode) await setMode(naiveMode.value);
    }
    reportToast('Configuration saved');
    // Capture before closing: if this save was part of a connect attempt, the host resumes it
    // so the user does not have to press Connect again.
    const resume = props.resumeConnect;
    close({ persistDraft: false });
    emit('saved', { resumeConnect: resume });
  } catch (e) {
    const msg = getErrMsg(e, '');
    if (/Enter\s+a\s+user\s+UUID/i.test(msg)) {
      markFieldError('uuid', 'Enter your UUID.');
      return;
    }
    if (/Invalid\s+user\s+UUID/i.test(msg)) {
      markFieldError('uuid', 'Use a standard UUID format (8-4-4-4-12 hex).');
      return;
    }
    if (/Enable\s+at\s+least\s+one\s+VPN\s+outbound/i.test(msg)) {
      reportError('Turn on at least one outbound above.');
      return;
    }
    reportError(msg || 'Save failed');
  }
}

/** NModal drives this on backdrop click and Esc; both keep the draft, like Cancel does. */
function onUpdateShow(value) {
  if (!value) close();
}
</script>

<template>
  <NModal
    :show="visible"
    preset="card"
    class="dialog dialog--wide"
    :bordered="false"
    title="VPN configuration"
    @update:show="onUpdateShow"
  >
    <div class="body">
      <NAlert v-if="resumeConnect" type="warning" :bordered="false" class="resume">
        A connection is waiting on this — save and it carries on by itself.
      </NAlert>

      <section class="field">
        <label class="field__label" for="vpn-user-uuid">User UUID</label>
        <p class="field__hint">
          The protocols below are loaded from the server's profile on each app start and when you connect; they all
          sign in with this UUID.
        </p>
        <NInput
          ref="uuidInput"
          v-model:value="uuid"
          placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
          :status="errorField === 'uuid' ? 'error' : undefined"
          :input-props="{ id: 'vpn-user-uuid', spellcheck: 'false', autocomplete: 'off' }"
          class="mono"
        />
      </section>

      <section class="field">
        <span class="field__label">Mode</span>
        <p class="field__hint">
          NaiveProxy runs on its own: it is the heaviest protocol, so it never joins the automatic selector next to
          the others. Use it when the protocols below are blocked or slow.
        </p>
        <div class="mode-grid" role="radiogroup" aria-label="Mode">
          <button
            type="button"
            class="toggle"
            role="radio"
            :aria-checked="!naiveMode"
            :class="{ 'toggle--on': !naiveMode }"
            @click="naiveMode = false"
          >
            <span class="toggle__label">Protocols</span>
          </button>
          <button
            type="button"
            class="toggle"
            role="radio"
            :aria-checked="naiveMode"
            :class="{ 'toggle--on': naiveMode }"
            @click="naiveMode = true"
          >
            <span class="toggle__label">NaiveProxy</span>
          </button>
        </div>
      </section>

      <section v-if="!naiveMode" class="field">
        <div class="field__head">
          <span class="field__label">{{ labels.outbounds }}</span>
          <span class="field__count">{{ enabledCount }} of {{ availableOutbounds.length }} on</span>
        </div>
        <p class="field__hint">
          Turn off protocols you do not want in the automatic selector (<code>{{ labels.urltest }}</code>). Every one of
          them changes the address your traffic leaves from; keep at least one on.
        </p>
        <div v-if="availableOutbounds.length" class="toggle-grid">
          <button
            v-for="tag in availableOutbounds"
            :key="tag"
            type="button"
            class="toggle"
            role="switch"
            :aria-checked="outboundEnabled[tag] !== false"
            @click="outboundEnabled = { ...outboundEnabled, [tag]: outboundEnabled[tag] === false }"
          >
            <NSwitch size="small" tabindex="-1" aria-hidden="true" :value="outboundEnabled[tag] !== false" />
            <span class="toggle__label">{{ outboundLabel(tag) }}</span>
          </button>
        </div>
        <p v-else class="field__empty">
          The sing-box profile has not been downloaded yet, so there is nothing to choose between. Connect once and it
          will list what the server offers.
        </p>
      </section>

      <p class="foot-note">
        {{
          standalone
            ? 'Save writes your UUID, mode and toggles to disk.'
            : 'Fields are saved as a draft while this window is open. Save writes your UUID, mode and toggles to disk.'
        }}
      </p>
    </div>

    <template #footer>
      <div class="footer">
        <NButton quaternary @click="close()">{{ labels.cancel }}</NButton>
        <NButton type="primary" :disabled="!hydrated" @click="save">Save configuration</NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
/*
 * Two columns, and the switch next to the name rather
 * than pushed to the far edge of a full-width row. Ten rows of one item each
 * filled the window; five rows of two fit without scrolling, and the switch is
 * where the eye already is.
 */
.toggle-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 4px 10px;
}

/*
 * The whole row is the control, so a click anywhere in it counts — the name is
 * a bigger target than the switch, and NSwitch renders no input, which is what
 * made an enclosing <label> do nothing.
 */
.toggle {
  display: flex;
  width: 100%;
  text-align: left;
  align-items: center;
  gap: 10px;
  min-width: 0;
  padding: 6px 10px;
  border-radius: 10px;
  cursor: pointer;
  background: var(--surface-sunken);
  border: 1px solid var(--border);
  transition: border-color 0.15s ease, background 0.15s ease;
}

.toggle:hover {
  background: var(--surface);
  border-color: var(--border-hover);
}

.mode-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 4px 10px;
}

.mode-grid .toggle {
  justify-content: center;
}

.toggle--on {
  border-color: #10b981;
  background: var(--surface);
}

.toggle--on .toggle__label {
  color: var(--text);
  font-weight: 600;
}

/* The switch shows the state; the row it sits in is what takes the click. */
.toggle :deep(.n-switch) {
  pointer-events: none;
}

.toggle__label {
  font-size: 13px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}


.body {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.resume {
  margin-bottom: 2px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.field__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.field__label {
  font-size: 14px;
  font-weight: 600;
  color: var(--text);
}

.field__count {
  font-size: 12px;
  color: var(--text-dim);
}

.field__hint {
  margin: 0 0 4px;
  font-size: 12px;
  line-height: 1.55;
  color: var(--text-dim);
}

.field__hint code,
.mono :deep(input) {
  font-family: var(--n-font-family-mono, monospace);
}

.field__hint code {
  font-size: 0.94em;
  color: var(--text-muted);
}

.field__empty {
  margin: 0;
  padding: 12px 14px;
  border-radius: 12px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-dim);
  background: var(--surface-sunken);
  border: 1px solid var(--border);
}

</style>

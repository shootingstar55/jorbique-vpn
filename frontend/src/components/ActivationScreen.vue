<script setup>
/**
 * The first-run screen: the app stays closed until the user enters a UUID the
 * server knows, the way a licence key works. The metrics are counted by that
 * UUID, and the check itself is the user's first push of them.
 *
 * Windows shows it inside the app window in place of everything else; macOS
 * shows it as the only window the menu bar opens until it is done (the
 * standalone panel, served by the menu bar's loopback origin). Closing it there
 * quits the app.
 */
import { ref } from 'vue';
import { NAlert, NButton, NInput } from 'naive-ui';
import { binding } from '../lib/bindings.js';
import { panelAPI } from '../lib/panel-api.js';
import { getErrMsg } from '../lib/errors.js';
import { explainActivationError, looksLikeUUID } from '../lib/activation.js';
import { labels } from '../lib/labels.js';
import ThemeSwitch from './ThemeSwitch.vue';

const props = defineProps({
  /** The macOS panel: no Wails bindings, and a Quit button of its own. */
  standalone: { type: Boolean, default: false },
});
const emit = defineEmits(['activated']);

const appIconUrl = 'app-icon.png';
const uuid = ref('');
const checking = ref(false);
const error = ref('');

// On macOS the menu bar keeps the choice for the windows opened after this one;
// Windows shares one origin, and ThemeSwitch's localStorage already covers it.
function rememberTheme(mode) {
  if (props.standalone) panelAPI.ask('theme', mode).catch(() => {});
}

async function activate() {
  if (checking.value) return;
  const value = uuid.value.trim();
  if (!looksLikeUUID(value)) {
    error.value = explainActivationError('Invalid user UUID');
    return;
  }
  checking.value = true;
  error.value = '';
  try {
    const native = binding('ActivateUserUUID');
    if (native) {
      await native(value);
    } else {
      await panelAPI.save('uuid', { uuid: value });
      await panelAPI.close();
    }
    emit('activated');
  } catch (e) {
    error.value = explainActivationError(getErrMsg(e, ''));
  } finally {
    checking.value = false;
  }
}
</script>

<template>
  <form class="activation" @submit.prevent="activate">
    <div class="activation__card">
      <img :src="appIconUrl" alt="" width="56" height="56" />
      <h1>Jorbique VPN Extended</h1>
      <p>Enter the user UUID you were given to start using the app.</p>

      <NInput
        v-model:value="uuid"
        class="activation__input"
        placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
        :disabled="checking"
        :status="error ? 'error' : undefined"
        :input-props="{ spellcheck: 'false', autocomplete: 'off', 'aria-label': 'User UUID' }"
        autofocus
        @keyup.enter="activate"
      />
      <NAlert v-if="error" type="error" :bordered="false" role="alert">{{ error }}</NAlert>

      <div class="activation__actions">
        <ThemeSwitch class="activation__theme" @change="rememberTheme" />
        <NButton v-if="props.standalone" quaternary :disabled="checking" @click="panelAPI.close()">
          {{ labels.quit }}
        </NButton>
        <NButton type="primary" attr-type="submit" :loading="checking" :disabled="!uuid.trim()">
          {{ labels.continue }}
        </NButton>
      </div>
    </div>
  </form>
</template>

<style scoped>
.activation {
  flex: 1;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  box-sizing: border-box;
}

.activation__card {
  width: 100%;
  max-width: 420px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  text-align: center;
}

.activation__card h1 {
  margin: 0;
  font-size: 20px;
  color: var(--text);
}

.activation__card p {
  margin: 0;
  font-size: 13px;
  color: var(--text-muted);
}

.activation__input {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

.activation__card :deep(.n-alert) {
  width: 100%;
  text-align: left;
}

.activation__actions {
  width: 100%;
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 8px;
}

.activation__theme {
  width: 186px;
  margin-right: auto;
}

/* No card under it here, and the dark rail is the window's own colour: the
   outline the input has keeps it visible. */
.activation__theme :deep(.n-tabs-rail) {
  box-shadow: inset 0 0 0 1px var(--border);
}
</style>

<script setup>
/**
 * The macOS main window: the same shape the Windows client has — a connect ring
 * with the status under it, a row of actions, and the VPN autostart switch — drawn
 * from the daemon's own status output rather than from Wails bindings.
 *
 * The menu bar stays; this is the window it opens.
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { NAlert, NButton, NCard, NIcon, NSpin, NSwitch, useMessage } from 'naive-ui';
import AppIcon from './AppIcon.vue';
import ClientUpdateNotice from './ClientUpdateNotice.vue';
import ConnectionCheck from './ConnectionCheck.vue';
import IpInfoCard from './IpInfoCard.vue';
import ProtocolList from './ProtocolList.vue';
import ThemeSwitch from './ThemeSwitch.vue';
import AccountModal from './AccountModal.vue';
import { panelAPI } from '../lib/panel-api.js';
import { getErrMsg } from '../lib/errors.js';
import { labels } from '../lib/labels.js';

const message = useMessage();

/** Bound, not literal: a literal src is turned into an import by the compiler. */
const appIconUrl = 'app-icon.png';

/**
 * Poll cadence. Idle is the menu bar's own; while something is in flight it is a
 * second, because that is when the window has something to show — a connect
 * takes seconds and used to look like nothing at all until it finished.
 */
const IDLE_MS = 5000;
const BUSY_MS = 1000;

const state = ref({
  vpns: [],
  running: [],
  exitIP: '',
  daemon: '',
  vpnAutostart: false,
  busy: false,
});
const loading = ref(true);
const error = ref('');
const acting = ref('');
let timer = null;
let refreshing = false;

/** Always arrays, whatever the bridge sent: reading .length off null blanked the card. */
const running = computed(() => state.value.running || []);
const vpns = computed(() => state.value.vpns || []);

/** What the profile offers, for the protocol list; the check's own list wins while connected. */
const outbounds = computed(() => state.value.outbounds || []);

const connected = computed(() => running.value.length > 0);

/**
 * What is in flight, in the client's own words. The press sets it so the ring
 * answers the click at once; after that the daemon's own answer takes over, so
 * the state survives a reload and shows a start begun from the menu bar too.
 */
const pending = computed(() => {
  if (acting.value === 'connect') return 'Ищем лучшее подключение';
  if (acting.value === 'disconnect') return 'Отключение';
  const reported = state.value.action || '';
  if (/^(Connecting|Restarting)$/.test(reported)) return 'Ищем лучшее подключение';
  if (reported === 'Disconnecting') return 'Отключение';
  if (reported === 'Quitting') return 'Выход';
  return '';
});

const ringState = computed(() => {
  if (pending.value) return 'is-pending';
  return connected.value ? 'is-on' : 'is-off';
});

const headline = computed(() => pending.value || (connected.value ? 'Подключено' : 'Отключено'));
const subline = computed(() => {
  if (pending.value) return 'Это займёт несколько секунд.';
  if (connected.value) return running.value.join(' · ');
  return 'Подключите VPN, чтобы трафик был защищён';
});

/**
 * The buttons, in the order both clients show them. The icons are the ones the
 * Windows screen draws, from the same component — a panel that opens the same
 * window should not be labelled differently on the two platforms.
 */
const ACTIONS = [
  { id: 'account', label: 'Личный кабинет', icon: 'user' },
  { id: 'set-uuid', label: 'Изменить UUID', icon: 'settings' },
  { id: 'route-rules', label: 'Исключения', icon: 'apps' },
  { id: 'logs', label: labels.logs, icon: 'logs' },
];

async function refresh() {
  if (refreshing) return;
  refreshing = true;
  try {
    const next = await panelAPI.load('main');
    if (next) state.value = next;
    error.value = '';
  } catch (e) {
    error.value = getErrMsg(e, 'Не удалось получить состояние VPN');
  } finally {
    loading.value = false;
    refreshing = false;
  }
}

async function act(action, payload) {
  if (acting.value) return;
  acting.value = action;
  try {
    await panelAPI.save('main', { action, value: payload });
    await refresh();
  } catch (e) {
    message.error(getErrMsg(e, 'Действие не выполнено'));
  } finally {
    acting.value = '';
  }
}

// The poll runs faster while anything is in flight, so the ring stops pulsing
// promptly rather than up to five seconds after the tunnel is up.
watch(pending, (busy) => {
  clearInterval(timer);
  timer = setInterval(refresh, busy ? BUSY_MS : IDLE_MS);
});

/**
 * The Logs window opens whatever the tunnel is doing: a connect that hangs is when
 * someone wants to read them. The other panels change settings, so they wait.
 */
const ALWAYS_OPEN = new Set(['logs', 'account']);

/** «Личный кабинет» opens inside this window, not as a panel of its own. */
const accountOpen = ref(false);

function openPanel(id) {
  if (id === 'account') {
    accountOpen.value = true;
    return;
  }
  if (!ALWAYS_OPEN.has(id)) {
    act('open', id);
    return;
  }
  panelAPI.save('main', { action: 'open', value: id }).catch((e) => {
    message.error(getErrMsg(e, 'Не удалось открыть окно'));
  });
}

function toggleConnection() {
  if (pending.value) return;
  act(connected.value ? 'disconnect' : 'connect');
}

onMounted(() => {
  refresh();
  timer = setInterval(refresh, IDLE_MS);
});
onBeforeUnmount(() => clearInterval(timer));
</script>

<template>
  <div class="main client-screen">
    <NCard class="pane pane--head" :bordered="false">
      <div class="head">
        <div class="head__logo" :class="{ 'is-protected': connected }">
          <img :src="appIconUrl" alt="" width="40" height="40" />
        </div>
        <div class="head__text">
          <h1>Jorbique VPN Extended</h1>
          <p class="head__note">Лучшее подключение выбирается автоматически</p>
        </div>
        <ThemeSwitch @change="(mode) => act('theme', mode)" />
      </div>
    </NCard>

    <NAlert v-if="error" type="error" :bordered="false">{{ error }}</NAlert>

    <NCard class="pane pane--hero" :bordered="false">
      <NSpin :show="loading && !vpns.length">
        <div class="hero">
          <button
            type="button"
            class="ring"
            :class="ringState"
            :disabled="!!pending"
            :aria-label="pending || (connected ? 'Отключить' : 'Подключить')"
            @click="toggleConnection"
          >
            <span class="ring__pulse" aria-hidden="true"></span>
            <NIcon :size="23"><AppIcon name="power" /></NIcon>
          </button>
          <ConnectionCheck v-if="pending && !['Отключение', 'Выход'].includes(pending)" :progress="state.connectionProgress" />
          <div v-else class="hero__text" aria-live="polite" aria-atomic="true">
            <h2 class="hero__headline">{{ headline }}</h2>
            <p class="hero__sub">{{ subline }}</p>
          </div>
        </div>
      </NSpin>
    </NCard>

    <IpInfoCard class="pane" :connected="connected" />

    <ProtocolList class="pane" :protocols="outbounds" :report="state.report" :connected="connected && !pending" />

    <NCard class="pane" :bordered="false">
      <div class="switches">
        <div class="switches__row">
          <span>
            <b id="vpn-autostart-label">Включать VPN при загрузке</b>
            <small>Служба подключит VPN сама, даже если никто не вошёл в систему.</small>
          </span>
          <NSwitch
            :value="state.vpnAutostart"
            aria-labelledby="vpn-autostart-label"
            :loading="acting === 'vpn-autostart'"
            @update:value="(on) => act('vpn-autostart', on)"
          />
        </div>
      </div>

      <div class="actions">
        <NButton
          v-for="action in ACTIONS"
          :key="action.id"
          tertiary
          :disabled="!ALWAYS_OPEN.has(action.id) && (!!acting || !!pending)"
          @click="openPanel(action.id)"
        >
          <template #icon><NIcon><AppIcon :name="action.icon" /></NIcon></template>
          {{ action.label }}
        </NButton>
      </div>

      <p class="daemon">{{ state.daemon || 'Служба: —' }}</p>
    </NCard>

    <ClientUpdateNotice />

    <AccountModal v-model:visible="accountOpen" @error="message.error($event)" @toast="message.success($event)" />
  </div>
</template>

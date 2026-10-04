<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { NButton, NCard, NIcon, NModal, NProgress, NSwitch, NTag } from 'naive-ui';
import AppIcon from './AppIcon.vue';
import IpInfoCard from './IpInfoCard.vue';
import ThemeSwitch from './ThemeSwitch.vue';
import HeroTraffic from './HeroTraffic.vue';
import { dashboardAPI } from '../lib/dashboard-api.js';
import { binding } from '../lib/bindings.js';
import { getErrMsg } from '../lib/errors.js';
import { labels } from '../lib/labels.js';
import { loadAvailableOutbounds, outboundLabel } from '../lib/outbounds.js';

const emit = defineEmits([
  'error', 'toast', 'openVpnConfig', 'openIgnoreApps', 'openLogs', 'openDashboard',
]);

/**
 * Served straight out of the public dir (vite.config.js points it at ../assets), so it is
 * bound rather than written as a literal src — a literal would be rewritten into a module
 * import at build time and fail to resolve, since the file is not under src/.
 */
const appIconUrl = 'app-icon.png';

/** How often the settled state is re-read from the backend. */
const STATUS_POLL_MS = 3000;
/** Grace period after Connect() resolves, before the status is treated as settled. */
const TUNNEL_SETTLE_MS = 2500;
/** How long a failed bootstrap leaves its message up before the UI carries on. */
const BOOTSTRAP_ERROR_MS = 2500;
/** OnStartup runs RefreshAutostartIfEnabled, so autostart is re-read once after launch. */
const AUTOSTART_RECHECK_MS = 1500;


const connected = ref(false);

/**
 * What the core has carried, for the columns beside the ring. Read from the same
 * API the dashboard uses, and only while a tunnel is up — with nothing running
 * there is no API to ask.
 */
const traffic = ref({ uplinkTotal: 0, downlinkTotal: 0 });

async function refreshTraffic() {
  if (!connected.value) {
    traffic.value = { uplinkTotal: 0, downlinkTotal: 0 };
    return;
  }
  try {
    const snapshot = await dashboardAPI.snapshot();
    traffic.value = {
      uplinkTotal: Number(snapshot?.status?.uplinkTotal) || 0,
      downlinkTotal: Number(snapshot?.status?.downlinkTotal) || 0,
    };
  } catch {
    /* the API comes and goes with the tunnel; the columns just hold their last value */
  }
}

/**
 * Tags from the downloaded profile, shown under the app name. Refreshed on launch and after
 * a connect — never from the status poll, because reading them re-downloads the profile.
 * @type {import('vue').Ref<string[]>}
 */
const availableOutbounds = ref([]);

/**
 * null while settled; otherwise the in-flight phase and the label to show for it. It replaces
 * both the old isTransitioning flag and the imperative status painting that ran beside it —
 * the ring, the headline and every disabled control read off this one value, so they cannot
 * disagree about whether something is in flight.
 * @type {import('vue').Ref<{ phase: 'connecting'|'disconnecting', text: string }|null>}
 */
const transition = ref(null);

/**
 * Re-entrancy guard for every connect/disconnect entry point. Deliberately a plain variable,
 * not a ref: it is set synchronously before the first await so a rapid double-click cannot
 * launch two flows, and nothing renders from it.
 */
let connectOpInFlight = false;

const autostartSupported = ref(true);
const autostartEnabled = ref(false);
const autostartSwitching = ref(false);

const bootstrapVisible = ref(false);
const bootstrapTitle = ref('Setting up sing-box');
const bootstrapLabel = ref('Downloading sing-box…');
const bootstrapPercent = ref(0);
let bootstrapInFlight = false;

const busy = computed(() => transition.value !== null);

const headline = computed(() => transition.value?.text ?? (connected.value ? 'Connected' : 'Disconnected'));
const subline = computed(() => {
  if (transition.value) return 'Hold on a moment';
  return connected.value ? 'Your traffic is protected' : 'Connect VPN to stay protected';
});

/** Drives the ring colour, the glow and the pulse in one value. */
const ringState = computed(() => {
  if (transition.value) return 'is-pending';
  return connected.value ? 'is-on' : 'is-off';
});

const protocols = computed(() => availableOutbounds.value.map(outboundLabel).join(' · '));

const primaryLabel = computed(() => {
  if (transition.value) return transition.value.phase === 'disconnecting' ? 'Disconnecting…' : 'Connecting…';
  return connected.value ? 'Disconnect' : 'Connect';
});

const autostartDisabled = computed(() => busy.value || autostartSwitching.value);

// --- Status ------------------------------------------------------------------------

async function refreshStatus(force = false) {
  if (transition.value && !force) return;

  dashboardSupported.value = Boolean(binding('DashboardSnapshot'));

  const isConnected = binding('IsConnected');
  if (!isConnected) return;
  try {
    connected.value = await isConnected();
  } catch {
    // A transient poll failure keeps the last known state until the next poll.
  }

  // Rides the status poll rather than a timer of its own: the columns read the
  // same cadence as everything else in the hero.
  await refreshTraffic();
}

async function refreshOutbounds() {
  // The NaiveProxy mode connects through naive alone, whatever else the profile offers.
  const get = binding('GetVpnUserConfig');
  let naive = false;
  try {
    naive = Boolean(get && (await get())?.naiveMode);
  } catch {
    // Without the saved config the protocol list is the better guess.
  }
  availableOutbounds.value = naive ? ['naive'] : await loadAvailableOutbounds();
}

// --- Autostart ---------------------------------------------------------------------

async function loadAutostart() {
  // Autostart is a Windows-only capability (macOS ships launchd autostart in the menu-bar app).
  // Default to supported so a missing binding never hides the row on Windows; only a backend
  // that positively reports "unsupported" hides it — otherwise macOS would surface an error
  // toast on every click of a control that can only ever fail.
  let supported = true;
  const isSupported = binding('IsAutostartSupported');
  if (isSupported) {
    try {
      supported = !!(await isSupported());
    } catch {
      // Older backends can reject capability checks but still support autostart.
      supported = true;
    }
  }
  autostartSupported.value = supported;
  if (!supported) {
    autostartEnabled.value = false;
    return;
  }

  let on = false;
  const isEnabled = binding('IsAutostartEnabled');
  if (isEnabled) {
    try {
      on = await isEnabled();
    } catch {
      // No confirmed autostart setting: keep the switch off.
    }
  }
  autostartEnabled.value = !!on;
}

/**
 * Whether the backend can open the dashboard. Re-read on every status poll rather than once
 * at setup: window.go is filled when the Wails runtime boots, which is after this component
 * is created, so a one-shot check at setup would hide the button for the whole session.
 */
const dashboardSupported = ref(false);

async function setAutostart(nextOn) {
  const set = binding('SetAutostartEnabled');
  if (!set) return;
  autostartSwitching.value = true;
  try {
    // Optimistic update before the await, so the switch never lags the click. The catch's
    // loadAutostart() puts it back to whatever the backend actually reports.
    autostartEnabled.value = nextOn;
    await set(nextOn);
    emit('toast', `Autostart ${nextOn ? 'enabled' : 'disabled'}`);
  } catch (err) {
    emit('error', getErrMsg(err, 'Failed to change autostart'));
    await loadAutostart();
  } finally {
    // Cleared only after the RPC settles, so a second click cannot fire while the first is
    // still in flight.
    autostartSwitching.value = false;
  }
}

// --- sing-box bootstrap ------------------------------------------------------------
// On launch the app downloads sing-box (if missing) or updates it (if the server has a newer
// build) behind a blocking overlay, then reveals the UI. There are no manual Install/Update
// buttons — the flow mirrors the macOS client. Percent arrives on the "singbox-progress" event.

function setSingboxProgress(pct) {
  const v = Math.max(0, Math.min(100, Math.round(Number(pct) || 0)));
  bootstrapPercent.value = v;
  bootstrapLabel.value = v >= 100 ? 'Installing…' : 'Downloading sing-box…';
}

/** Downloads (install or update) behind the overlay. On failure it shows a short message and
 *  proceeds; the next launch retries (no manual button). */
async function runSingboxBootstrap(mode) {
  if (bootstrapInFlight) return false;
  bootstrapInFlight = true;

  const isUpdate = mode === 'update';
  const run = binding(isUpdate ? 'UpdateSingbox' : 'InstallSingbox');
  if (!run) {
    bootstrapInFlight = false;
    return false;
  }

  bootstrapTitle.value = isUpdate ? 'Updating sing-box' : 'Setting up sing-box';
  bootstrapLabel.value = isUpdate ? 'Downloading update…' : 'Downloading sing-box…';
  bootstrapPercent.value = 0;
  bootstrapVisible.value = true;

  let ok = false;
  try {
    await run();
    ok = true;
  } catch (e) {
    bootstrapLabel.value = `${getErrMsg(e, 'Download failed')} — will retry on next launch.`;
    await new Promise((r) => setTimeout(r, BOOTSTRAP_ERROR_MS));
  } finally {
    // Always clear the guard — otherwise a thrown error would wedge it true and block every
    // later install/update attempt for the rest of the session.
    bootstrapInFlight = false;
  }

  bootstrapVisible.value = false;
  return ok;
}

/** Runs once at launch: install if missing, else update if the server build is newer. */
async function bootstrapSingbox() {
  const isInstalled = binding('IsSingboxInstalled');
  if (!isInstalled) return;

  let installed = false;
  try {
    installed = await isInstalled();
  } catch {
    // Installation is unconfirmed; the installer below validates or repairs it.
  }
  if (!installed) {
    await runSingboxBootstrap('install');
    return;
  }

  let updateAvailable = false;
  const hasUpdate = binding('IsSingboxUpdateAvailable');
  if (hasUpdate) {
    try {
      updateAvailable = await hasUpdate();
    } catch {
      // Offline update checks must not prevent use of an installed core.
    }
  }
  if (updateAvailable) await runSingboxBootstrap('update');
}

async function ensureSingboxReadyForConnect() {
  const isInstalled = binding('IsSingboxInstalled');
  if (!isInstalled) return true;
  let installed = false;
  try {
    installed = await isInstalled();
  } catch {
    // Installation is unconfirmed; the installer below validates or repairs it.
  }
  if (installed) return true;
  // Bootstrap normally installs sing-box on launch; if it is still missing (e.g. that
  // download failed), fetch it now behind the overlay.
  return await runSingboxBootstrap('install');
}

// --- Connect / disconnect ----------------------------------------------------------

async function ensureVpnConfigForConnect() {
  const hasConfig = binding('HasVpnUserConfig');
  try {
    if (!(hasConfig && (await hasConfig()))) {
      emit('openVpnConfig', { resumeConnect: true });
      return false;
    }
  } catch {
    // Let the user repair a missing or unreadable profile in the config window.
    emit('openVpnConfig', { resumeConnect: true });
    return false;
  }
  return true;
}

async function runConnect() {
  const connect = binding('Connect');
  if (!connect) return;

  const isConnected = binding('IsConnected');
  const wasConnected = !!(isConnected && (await isConnected()));

  if (wasConnected) {
    transition.value = { phase: 'disconnecting', text: 'Disconnecting' };
    try {
      await binding('Disconnect')();
    } catch (e) {
      transition.value = null;
      refreshStatus(true);
      emit('error', getErrMsg(e, 'Disconnect failed'));
      return;
    }
  }

  transition.value = {
    phase: 'connecting',
    text: wasConnected ? 'Reconnecting' : 'Connecting',
  };

  try {
    await connect();
    transition.value = { phase: 'connecting', text: 'Establishing tunnel' };
    await new Promise((r) => setTimeout(r, TUNNEL_SETTLE_MS));
    transition.value = null;
    refreshStatus(true);
    void refreshOutbounds();
  } catch (e) {
    transition.value = null;
    refreshStatus(true);
    const msg = getErrMsg(e, 'Connect failed');
    let hasConfig = true;
    try {
      const has = binding('HasVpnUserConfig');
      hasConfig = !!(has && (await has()));
    } catch {
      // Open the repair form when the failed connect also left its config unreadable.
      hasConfig = false;
    }
    if (!hasConfig || /required|configure|VLESS|config/i.test(msg)) {
      emit('openVpnConfig', { resumeConnect: true });
    } else {
      emit('error', msg);
    }
  }
}

async function onConnect() {
  if (connectOpInFlight) return;
  connectOpInFlight = true;
  try {
    if (!(await ensureSingboxReadyForConnect())) return;
    if (!(await ensureVpnConfigForConnect())) return;
    await runConnect();
  } finally {
    connectOpInFlight = false;
  }
}

async function onDisconnect() {
  const disconnect = binding('Disconnect');
  if (!disconnect) return;
  if (connectOpInFlight) return;
  connectOpInFlight = true;

  transition.value = { phase: 'disconnecting', text: 'Disconnecting' };
  try {
    await disconnect();
    transition.value = null;
    refreshStatus(true);
  } catch (e) {
    transition.value = null;
    refreshStatus(true);
    emit('error', getErrMsg(e, 'Disconnect failed'));
  } finally {
    connectOpInFlight = false;
  }
}

/** The one primary action: what it does depends on where the tunnel currently is. */
function onPrimaryAction() {
  if (busy.value) return;
  if (connected.value) void onDisconnect();
  else void onConnect();
}

// --- Imperative API for the host ---------------------------------------------------

/** Resumes a connect the user already started but which stopped for want of a config. */
async function resumeConnect() {
  if (connectOpInFlight) return;
  connectOpInFlight = true;
  try {
    if (!(await ensureSingboxReadyForConnect())) return;
    await runConnect();
  } finally {
    connectOpInFlight = false;
  }
}

/**
 * Reconnects if a tunnel is up, so sing-box reloads its config — used
 * after the ignore list changed. Routed through runConnect so the controls and the
 * status poll stay managed; otherwise the poll fires in the disconnect→connect gap, repaints
 * "Disconnected" and lets a second concurrent connect start.
 */
async function reconnectIfConnected() {
  try {
    const isConnected = binding('IsConnected');
    const conn = !!(isConnected && (await isConnected()));
    if (!conn || !binding('Disconnect') || !binding('Connect') || connectOpInFlight) return;

    connectOpInFlight = true;
    try {
      await runConnect();
    } finally {
      connectOpInFlight = false;
    }
  } catch {
    // Only the status reads can throw here (runConnect reports its own failures);
    // the list is already saved, so the tunnel picks it up on the next connect.
  }
}

defineExpose({ resumeConnect, reconnectIfConnected });

// --- Lifecycle ---------------------------------------------------------------------

let pollTimer = null;
let autostartRecheckTimer = null;
let offProgress = null;
let fitTimer = null;
let shellObserver = null;

/**
 * Tell the host how tall the screen actually is, so the window can be that tall.
 *
 * Guessing it from the layout was wrong every time: the same markup renders
 * taller under display scaling and a larger system text size, and the page is
 * the only place that knows the result. Absent on macOS, where the panel sizes
 * its own window — the binding is simply not there, and this does nothing.
 */
function fitWindowToContent() {
  const fit = binding('FitWindowToContent');
  if (!fit) return;
  const shell = document.querySelector('.shell');
  if (!shell) return;
  fit(Math.ceil(shell.scrollHeight));
}

function scheduleFit() {
  clearTimeout(fitTimer);
  fitTimer = setTimeout(fitWindowToContent, 150);
}

onMounted(() => {
  if (window.runtime && typeof window.runtime.EventsOn === 'function') {
    offProgress = window.runtime.EventsOn('singbox-progress', setSingboxProgress);
  }

  // First: download sing-box if missing / update it if the server build is newer, behind the
  // bootstrap overlay; then bring up the rest of the UI.
  void bootstrapSingbox();
  void loadAutostart();
  autostartRecheckTimer = setTimeout(() => void loadAutostart(), AUTOSTART_RECHECK_MS);

  void refreshStatus();
  void refreshOutbounds();
  pollTimer = setInterval(() => void refreshStatus(), STATUS_POLL_MS);

  // The IP card fills in later and the protocol line wraps or does not, so the
  // height is not settled at mount: watch it instead of measuring once.
  scheduleFit();
  const shell = document.querySelector('.shell');
  if (shell && typeof ResizeObserver === 'function') {
    shellObserver = new ResizeObserver(scheduleFit);
    shellObserver.observe(shell.firstElementChild || shell);
  }
});

onBeforeUnmount(() => {
  clearInterval(pollTimer);
  clearTimeout(autostartRecheckTimer);
  clearTimeout(fitTimer);
  if (shellObserver) shellObserver.disconnect();
  if (typeof offProgress === 'function') offProgress();
});
</script>

<template>
  <div class="screen">
    <!-- Identity -->
    <NCard class="pane" :bordered="false">
      <div class="brand">
        <div class="brand__logo" :class="{ 'is-protected': connected }">
          <img :src="appIconUrl" alt="" width="48" height="48" />
        </div>
        <div class="brand__text">
          <h1>Jorbique VPN</h1>
          <p v-if="protocols" :title="protocols">{{ protocols }}</p>
          <p v-else class="brand__pending">Protocol list loads with the profile</p>
        </div>
        <ThemeSwitch class="brand__theme" />
        <NTag class="brand__chip" type="success" round size="small">sing-box client</NTag>
      </div>
    </NCard>

    <!-- Connection -->
    <NCard class="pane pane--hero" :bordered="false">
      <div class="hero">
        <div class="hero-row">
          <HeroTraffic direction="down" :total="traffic.downlinkTotal" :active="connected" />
          <button
            type="button"
            class="ring"
            :class="ringState"
            :disabled="busy"
            :aria-label="primaryLabel"
            @click="onPrimaryAction"
          >
            <span class="ring__pulse" aria-hidden="true"></span>
            <NIcon :size="46" class="ring__icon"><AppIcon name="power" /></NIcon>
          </button>
          <HeroTraffic direction="up" :total="traffic.uplinkTotal" :active="connected" />
        </div>

        <div class="hero__text" aria-live="polite" aria-atomic="true">
          <p class="hero__headline">{{ headline }}</p>
          <p class="hero__subline">{{ subline }}</p>
        </div>
      </div>
    </NCard>

    <IpInfoCard class="pane" :connected="connected" />

    <!-- Settings and actions -->
    <NCard class="pane" :bordered="false">
      <div v-if="autostartSupported" class="setting">
        <div class="setting__text">
          <p class="setting__title">Start with Windows</p>
        </div>
        <NSwitch
          :value="autostartEnabled"
          :disabled="autostartDisabled"
          :loading="autostartSwitching"
          @update:value="setAutostart"
        />
      </div>

      <div class="actions">
        <NButton v-if="dashboardSupported" quaternary :disabled="!connected" @click="emit('openDashboard')">
          <template #icon><NIcon><AppIcon name="monitor" /></NIcon></template>
          {{ labels.dashboard }}
        </NButton>
        <NButton quaternary @click="emit('openVpnConfig')">
          <template #icon><NIcon><AppIcon name="settings" /></NIcon></template>
          VPN Config
        </NButton>
        <NButton quaternary @click="emit('openIgnoreApps')">
          <template #icon><NIcon><AppIcon name="apps" /></NIcon></template>
          Ignore apps/domains
        </NButton>
        <NButton quaternary @click="emit('openLogs')">
          <template #icon><NIcon><AppIcon name="logs" /></NIcon></template>
          {{ labels.logs }}
        </NButton>
      </div>
    </NCard>

    <!-- sing-box bootstrap: blocking, no way out; the next launch retries on failure -->
    <NModal
      :show="bootstrapVisible"
      preset="card"
      size="small"
      class="dialog dialog--bootstrap"
      :bordered="false"
      :closable="false"
      :mask-closable="false"
      :close-on-esc="false"
    >
      <div class="bootstrap">
        <div class="brand__logo">
          <img :src="appIconUrl" alt="" width="48" height="48" />
        </div>
        <p class="bootstrap__title">{{ bootstrapTitle }}</p>
        <p class="bootstrap__label">{{ bootstrapLabel }}</p>
        <NProgress
          type="line"
          :percentage="bootstrapPercent"
          :show-indicator="false"
          :height="8"
          border-radius="9999px"
        />
      </div>
    </NModal>
  </div>
</template>

<style scoped>
/*
 * The window is resizable, so the screen follows it: the cap was fixed at 620px,
 * which left the sides of a widened window empty and the content stranded in the
 * middle. It still stops growing eventually — a 2000px-wide row of two switches
 * reads worse than a column — but the ceiling is the window, not a number from
 * the first layout.
 */
.screen {
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;
  max-width: min(100%, 1100px);
  margin: 0 auto;
}

/* Naive's card gives the surface and radius; the blur and the lift are ours. */
.pane {
  backdrop-filter: blur(18px);
  box-shadow: 0 30px 60px -30px var(--surface-sunken);
  border: 1px solid rgba(148, 163, 184, 0.12);
}

.pane--hero {
  border-color: rgba(148, 163, 184, 0.16);
}

/* --- Identity --- */

.brand__theme {
  flex-shrink: 0;
  width: 186px;
}

.brand {
  display: flex;
  align-items: center;
  gap: 14px;
}

.brand__logo {
  width: 34px;
  height: 40px;
  flex-shrink: 0;
}

.brand__logo img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  transition: filter 0.35s ease, transform 0.35s ease;
}

/* The yellow/red glow the old header had, kept as the one brand flourish. */
.brand__logo.is-protected img {
  filter: drop-shadow(0 0 10px rgba(255, 206, 0, 0.55)) drop-shadow(0 0 14px rgba(255, 28, 3, 0.4));
  transform: scale(1.04);
}

.brand__text {
  min-width: 0;
  flex: 1;
}

.brand__text h1 {
  margin: 0;
  font-size: 17px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--text);
}

.brand__text p {
  margin: 2px 0 0;
  font-size: 12px;
  color: var(--text-dim);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.brand__pending {
  font-style: italic;
  opacity: 0.75;
}

.brand__chip {
  flex-shrink: 0;
}

/* --- Connection --- */

.hero {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 0;
}

/* The ring between the two traffic columns, which is what used to be blank
   space on either side of it. */
.hero-row {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  gap: 12px;
  width: 100%;
}

/* Shape, motion and the connected glow are in styles.css; this window's
   surface and resting colours are its own. */
.ring {
  background: radial-gradient(circle at 50% 35%, var(--surface), var(--surface-sunken));
  color: var(--ring, var(--text-dim));
}

.ring.is-off {
  --ring: var(--border);
  color: var(--text-dim);
}

.ring.is-on {
  color: #34d399;
}

.ring.is-pending {
  color: #fbbf24;
}

.hero__text {
  text-align: center;
}

.hero__headline {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  letter-spacing: -0.02em;
  color: var(--text);
}

.hero__subline {
  margin: 2px 0 0;
  font-size: 13px;
  color: var(--text-muted);
}

/* --- Settings and actions --- */

.setting {
  display: flex;
  align-items: center;
  gap: 14px;
  padding-bottom: 8px;
  margin-bottom: 8px;
  border-bottom: 1px solid var(--border);
}

.setting__text {
  flex: 1;
  min-width: 0;
}

.setting__title {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--text);
}


.actions {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
}

/* A window this short is one the screen would not give more of: 1080p at 150%
   leaves 624 logical pixels, and the layout above needs 671. Rather than a
   scrollbar, the same screen gets denser — the ring, the headline and the
   padding come down together, which is the last 47px. */
@media (max-height: 700px) {
  .ring {
    width: 64px;
    height: 64px;
  }

  .hero {
    gap: 6px;
  }

  .hero__headline {
    font-size: 16px;
  }

  .screen :deep(.n-card__content) {
    padding: 6px 12px !important;
  }

  .screen {
    gap: 4px;
  }

  .setting {
    padding-bottom: 6px;
    margin-bottom: 6px;
  }

  .actions :deep(.n-button) {
    min-height: 26px;
  }

  .hero-row {
    gap: 8px;
  }

  /* The line under the headline repeats what the ring already shows, and it is
     the first thing to go when the screen is this short. */
  .hero__subline {
    display: none;
  }
}

@media (min-width: 560px) {
  .actions {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

/* The longest label ("Ignore apps/domains") sets the width here: at the default
   button size it overflowed its own pill. Smaller type and tighter padding fit it
   whole rather than clipping it or wrapping the row to four columns. */
.actions :deep(.n-button) {
  font-size: 13px;
  padding: 3px 8px;
  height: auto;
  min-height: 30px;
}

/* The label sits in a fixed-height pill, so a descender (the tail of a "g" or a
   "p") lands below the line box and gets clipped. Room to breathe instead of a
   clip: the content is allowed its own line height and the pill grows to match. */
.actions :deep(.n-button__content) {
  min-width: 0;
  white-space: nowrap;
  line-height: 1.5;
}

/* --- Bootstrap --- */

.bootstrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding-bottom: 6px;
  text-align: center;
}

.bootstrap .brand__logo {
  margin-bottom: 6px;
}

.bootstrap__title {
  margin: 0;
  font-size: 17px;
  font-weight: 600;
  color: var(--text);
}

.bootstrap__label {
  margin: 0 0 14px;
  font-size: 13px;
  color: var(--text-muted);
}

.bootstrap :deep(.n-progress) {
  width: 100%;
}
</style>

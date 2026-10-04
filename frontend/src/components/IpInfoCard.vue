<!-- Copyright (c) 2026 Mancala Gaming. All rights reserved. -->
<script setup>
/**
 * Where the traffic comes out, and what it looks like from the outside: the
 * address, the network behind it, and the place it is attributed to.
 *
 * The same card on both clients — Windows reads it through its Wails binding,
 * macOS through the window's bridge — because the answer comes from one place
 * in Go either way.
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { NButton, NCard, NIcon, NSpin, NTooltip } from 'naive-ui';
import { binding } from '../lib/bindings.js';
import { panelAPI } from '../lib/panel-api.js';
import { getErrMsg } from '../lib/errors.js';

const props = defineProps({
  /** Re-checked whenever this changes: a connect or a disconnect is a new address. */
  connected: { type: Boolean, default: false },
});

/** The interval clash-verge uses, and the one the footer counts down. */
const AUTO_REFRESH_S = 300;
/**
 * When to look again after a tunnel comes up or goes down.
 *
 * "Running" means the process is alive, which is not the same as its routes
 * being in place — the first look after a connect regularly leaves through the
 * old path and answers with the address the user was trying to change. Waiting
 * longer is not a fix either: how long the routes take is not ours to know.
 *
 * So the card asks again until the answer actually changes, backing off as it
 * goes, and stops as soon as it does.
 */
const SETTLE_STEPS_MS = [2500, 5000, 10000, 20000];

const info = ref(null);
const loading = ref(false);
/**
 * When the next automatic look is due, as a clock reading rather than a counter
 * ticked once a second: a window that is not in front has its timers throttled —
 * to once a minute, and to nothing at all while it is hidden — and a counter
 * that decrements would then show a number that is simply wrong.
 */
const dueAt = ref(Date.now() + AUTO_REFRESH_S * 1000);
const now = ref(Date.now());
const error = ref('');
/** The address is hidden until asked for: a screenshot of this window is a common thing to share. */
const revealed = ref(false);
let timer = null;
/** Incremented to abandon a settle sequence a newer one has replaced. */
let settleToken = 0;

/**
 * The flag is an image, not an emoji: Windows ships no country flags in its
 * emoji font — deliberately — so a regional-indicator pair renders there as the
 * two letters it is built from. The SVGs are vendored (assets/flags), named by
 * the same lowercase code the lookup answers with, so the path is built from the
 * answer and there is no table to keep in step.
 */
const countryCode = computed(() => {
  const code = (info.value?.countryCode || '').trim().toUpperCase();
  return /^[A-Z]{2}$/.test(code) ? code : '';
});

/** Set when an image fails to load, so the code shows as a plate instead. */
const flagMissing = ref(false);
watch(countryCode, () => {
  flagMissing.value = false;
});

const flagSrc = computed(() => (countryCode.value ? `flags/${countryCode.value.toLowerCase()}.svg` : ''));

const country = computed(() => info.value?.country || info.value?.countryCode || '—');
const location = computed(() => [info.value?.city, info.value?.region].filter(Boolean).join(', ') || '—');
const coordinates = computed(() => {
  const item = info.value;
  if (!item || (!item.latitude && !item.longitude)) return '';
  return [item.countryCode, item.longitude?.toFixed(2), item.latitude?.toFixed(2)].filter(Boolean).join(', ');
});
const maskedIP = computed(() => '•'.repeat(Math.min((info.value?.ip || '').length, 12)) + '…');

/** "4:58", the way a countdown reads. */
const countdown = computed(() => {
  const total = Math.max(Math.round((dueAt.value - now.value) / 1000), 0);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`;
});

async function load(force = false) {
  if (loading.value) return;
  loading.value = true;
  try {
    const native = binding('GetIPInfo');
    info.value = native ? await native(force) : await panelAPI.ask('ipinfo', { force });
    error.value = '';
  } catch (e) {
    error.value = getErrMsg(e, 'Could not read the exit address');
  } finally {
    loading.value = false;
    dueAt.value = Date.now() + AUTO_REFRESH_S * 1000;
    now.value = Date.now();
  }
}

// A tunnel going up or down changes the answer, so this asks again rather than
// waiting out the interval — and keeps asking until the answer moves.
watch(() => props.connected, settleAfterTunnelChange);

async function settleAfterTunnelChange() {
  const token = ++settleToken;
  const before = info.value?.ip || '';
  for (const delay of SETTLE_STEPS_MS) {
    await new Promise((resolve) => setTimeout(resolve, delay));
    if (token !== settleToken) return;
    await load(true);
    if (token !== settleToken) return;
    if (info.value?.ip && info.value.ip !== before) return;
  }
}

// A hidden window's timers stop entirely, so the reading is caught up the moment
// it comes back rather than at the next tick.
function onVisibilityChange() {
  if (document.visibilityState !== 'visible') return;
  now.value = Date.now();
  if (now.value >= dueAt.value) load(true);
}

onMounted(() => {
  load();
  document.addEventListener('visibilitychange', onVisibilityChange);
  // One tick, which both counts down and does the refresh when it reaches zero:
  // a separate interval for the lookup would drift away from the number on screen.
  timer = setInterval(() => {
    now.value = Date.now();
    if (now.value >= dueAt.value) load(true);
  }, 1000);
});
onBeforeUnmount(() => {
  clearInterval(timer);
  settleToken++;
  document.removeEventListener('visibilitychange', onVisibilityChange);
});
</script>

<template>
  <NCard class="ipinfo" :bordered="false">
    <div class="ipinfo__head">
      <span class="ipinfo__badge">
        <NIcon :size="18">
          <svg viewBox="0 0 24 24">
            <path fill="currentColor"
                  d="M12 2a7 7 0 0 0-7 7c0 5.25 7 13 7 13s7-7.75 7-13a7 7 0 0 0-7-7m0 9.5A2.5 2.5 0 1
                     1 14.5 9A2.5 2.5 0 0 1 12 11.5" />
          </svg>
        </NIcon>
      </span>
      <h1>IP Information</h1>
      <NButton quaternary circle size="small" :loading="loading" title="Check again" @click="load(true)">
        <NIcon :size="18">
          <svg viewBox="0 0 24 24">
            <path fill="currentColor"
                  d="M17.65 6.35A8 8 0 1 0 19.73 14h-2.08A6 6 0 1 1 12 6c1.66 0 3.14.69 4.22 1.78L13
                     11h7V4z" />
          </svg>
        </NIcon>
      </NButton>
    </div>

    <NSpin :show="loading && !info">
      <p v-if="error" class="ipinfo__error">{{ error }}</p>
      <div v-else class="ipinfo__grid">
        <div class="ipinfo__left">
          <p class="ipinfo__country">
            <img
              v-if="flagSrc && !flagMissing"
              class="ipinfo__flag"
              width="30"
              height="22"
              :src="flagSrc"
              :alt="countryCode"
              @error="flagMissing = true"
            />
            <span v-else-if="countryCode" class="ipinfo__code">{{ countryCode }}</span>
            {{ country }}
          </p>
          <p class="ipinfo__row">
            <span class="ipinfo__key">IP:</span>
            <span class="ipinfo__value mono">{{ revealed ? (info?.ip || '—') : maskedIP }}</span>
            <NTooltip trigger="hover">
              <template #trigger>
                <button type="button" class="ipinfo__eye" @click="revealed = !revealed">
                  <NIcon :size="16">
                    <svg viewBox="0 0 24 24">
                      <path fill="currentColor"
                            d="M12 5c-5 0-9 4.5-10 7c1 2.5 5 7 10 7s9-4.5 10-7c-1-2.5-5-7-10-7m0 12a5 5 0 1 1
                               0-10a5 5 0 0 1 0 10m0-8a3 3 0 1 0 0 6a3 3 0 0 0 0-6" />
                    </svg>
                  </NIcon>
                </button>
              </template>
              {{ revealed ? 'Hide the address' : 'Show the address' }}
            </NTooltip>
          </p>
          <p class="ipinfo__row">
            <span class="ipinfo__key">ASN:</span>
            <span class="ipinfo__value">{{ info?.asn || '—' }}</span>
          </p>
        </div>
        <div class="ipinfo__right">
          <p class="ipinfo__row">
            <span class="ipinfo__key">ISP:</span>
            <span class="ipinfo__value">{{ info?.isp || '—' }}</span>
          </p>
          <p class="ipinfo__row">
            <span class="ipinfo__key">ORG:</span>
            <span class="ipinfo__value">{{ info?.org || '—' }}</span>
          </p>
          <p class="ipinfo__row">
            <span class="ipinfo__key">Location:</span>
            <span class="ipinfo__value">{{ location }}</span>
          </p>
          <p class="ipinfo__row">
            <span class="ipinfo__key">Timezone:</span>
            <span class="ipinfo__value">{{ info?.timezone || '—' }}</span>
          </p>
        </div>
      </div>
    </NSpin>

    <div class="ipinfo__foot">
      <span>Auto refresh in {{ countdown }}</span>
      <span>{{ coordinates }}</span>
    </div>
  </NCard>
</template>

<style scoped>
.ipinfo {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 14px;
}

.ipinfo :deep(.n-card__content) {
  padding: 10px 12px;
}

.ipinfo__head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding-bottom: 4px;
  margin-bottom: 6px;
  border-bottom: 1px solid var(--border);
}

.ipinfo__head h1 {
  flex: 1;
  margin: 0;
  font-size: 14px;
  color: var(--text);
}

.ipinfo__badge {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border-radius: 10px;
  color: #38bdf8;
  background: rgba(56, 189, 248, 0.14);
}

.ipinfo__grid {
  display: grid;
  grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
  gap: 4px 18px;
}

.ipinfo__country {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 0 0 6px;
  font-size: 16px;
  font-weight: 600;
  color: var(--text);
}

.ipinfo__flag {
  width: 30px;
  height: 22px;
  object-fit: cover;
  border-radius: 3px;
  /* A white flag on a light card would otherwise have no edge at all. */
  box-shadow: 0 0 0 1px var(--border);
}

/* The stand-in when an image is missing: a plate, not two loose letters. */
.ipinfo__code {
  padding: 2px 7px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.06em;
  color: #38bdf8;
  background: rgba(56, 189, 248, 0.14);
  border: 1px solid rgba(56, 189, 248, 0.3);
}

.ipinfo__row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin: 0 0 4px;
  font-size: 12.5px;
  min-width: 0;
}

.ipinfo__key {
  flex-shrink: 0;
  color: var(--text-muted);
}

.ipinfo__value {
  color: var(--text);
  overflow-wrap: anywhere;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

.ipinfo__eye {
  display: inline-grid;
  place-items: center;
  padding: 0;
  border: 0;
  background: none;
  cursor: pointer;
  color: var(--text-muted);
}

.ipinfo__eye:hover {
  color: var(--text);
}

.ipinfo__error {
  margin: 0;
  font-size: 13px;
  color: #f43f5e;
}

.ipinfo__foot {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-top: 6px;
  padding-top: 8px;
  border-top: 1px solid var(--border);
  font-size: 12px;
  color: var(--text-dim);
}

/* Same short-window case as the main screen: the card gives up its own air
   before the layout gives up a row. */
@media (max-height: 700px) {
  .ipinfo__head {
    padding-bottom: 3px;
    margin-bottom: 4px;
  }

  .ipinfo__grid {
    gap: 2px 18px;
  }

  .ipinfo__foot {
    margin-top: 4px;
  }
}
</style>

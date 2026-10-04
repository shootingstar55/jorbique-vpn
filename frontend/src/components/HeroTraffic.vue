<script setup>
/**
 * The two columns beside the connect ring: what the tunnel has carried, up and
 * down. The space was empty on both clients, and this is what the VPN clients
 * that fill it show — the numbers are already in the core's API, so it costs a
 * field in the poll rather than anything new to measure.
 *
 * Rates are derived from the totals between polls rather than read: the API
 * reports a rate too, but sampling it at the window's own cadence makes the two
 * halves of the line agree with each other.
 */
import { computed, ref, watch } from 'vue';
import { formatBytes } from '../lib/format.js';

const props = defineProps({
  direction: { type: String, required: true }, // 'up' | 'down'
  total: { type: Number, default: 0 },
  active: { type: Boolean, default: false },
});

const rate = ref(0);
let previous = null;

watch(
  () => props.total,
  (total) => {
    const now = Date.now();
    if (!props.active) {
      previous = null;
      rate.value = 0;
      return;
    }
    if (previous && total >= previous.total) {
      const seconds = Math.max(0.5, (now - previous.at) / 1000);
      rate.value = (total - previous.total) / seconds;
    }
    previous = { at: now, total };
  },
  { immediate: true },
);

const label = computed(() => (props.direction === 'up' ? 'Upload' : 'Download'));
const arrow = computed(() => (props.direction === 'up' ? '↑' : '↓'));
const rateText = computed(() => (props.active ? `${formatBytes(rate.value)}/s` : '—'));
const totalText = computed(() => (props.active ? `${formatBytes(props.total)} total` : 'Not connected'));
</script>

<template>
  <div class="traffic" :class="`traffic--${direction}`">
    <span class="traffic__label">{{ arrow }} {{ label }}</span>
    <strong class="traffic__rate">{{ rateText }}</strong>
    <span class="traffic__total">{{ totalText }}</span>
  </div>
</template>

<style scoped>
.traffic {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  text-align: center;
}

.traffic__label {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.09em;
  text-transform: uppercase;
  color: var(--text-dim);
}

.traffic__rate {
  font-size: 17px;
  font-weight: 650;
  letter-spacing: -0.02em;
  color: var(--text);
  font-variant-numeric: tabular-nums;
}

.traffic--up .traffic__rate {
  color: #38bdf8;
}

.traffic--down .traffic__rate {
  color: #10b981;
}

.traffic__total {
  font-size: 11px;
  color: var(--text-dim);
  white-space: nowrap;
}
</style>

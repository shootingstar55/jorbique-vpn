<script setup>
/**
 * Every protocol the server profile offers, as one compact row of chips, and while a tunnel
 * is up what the connection check made of each: the one the tunnel started on is lit, the
 * ones it can fail over to are plain, and the ones that did not work are dimmed.
 *
 * Both windows draw it: Windows from its bindings, macOS from the daemon's status.
 */
import { computed } from 'vue';
import { NCard } from 'naive-ui';
import { protocolRows } from '../lib/outbounds.js';

const props = defineProps({
  /** The profile's protocol tags. */
  protocols: { type: Array, default: () => [] },
  /** The last connection check while connected, else null. */
  report: { type: Object, default: null },
  connected: { type: Boolean, default: false },
});

const live = computed(() => (props.connected && props.report?.active ? props.report : null));
const rows = computed(() => protocolRows(props.protocols, live.value));
const active = computed(() => rows.value.find((row) => row.state === 'active'));

const summary = computed(() => {
  if (active.value) return `Сейчас: ${active.value.label}`;
  if (!rows.value.length) return 'Профиль ещё не загружен';
  return `С сервера: ${rows.value.length}`;
});
</script>

<template>
  <NCard class="protocols" :bordered="false">
    <div class="protocols__head">
      <span class="protocols__title">Протоколы</span>
      <span class="protocols__summary" :class="{ 'is-live': active }">{{ summary }}</span>
    </div>
    <ul v-if="rows.length" class="protocols__list">
      <li
        v-for="row in rows"
        :key="row.tag"
        class="chip"
        :class="row.state && `is-${row.state}`"
        :title="row.hint"
      >
        <i aria-hidden="true" />{{ row.label }}<span v-if="row.note" class="sr-only">, {{ row.note }}</span>
      </li>
    </ul>
  </NCard>
</template>

<style scoped>
.protocols__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

.protocols__title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
}

.protocols__summary {
  min-width: 0;
  font-size: 12px;
  color: var(--text-dim);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.protocols__summary.is-live {
  color: #10b981;
  font-weight: 600;
}

.protocols__list {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin: 6px 0 0;
  padding: 0;
  list-style: none;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border: 1px solid var(--border);
  border-radius: 9999px;
  background: var(--surface-sunken);
  font-size: 11px;
  line-height: 1.5;
  color: var(--text-muted);
  white-space: nowrap;
}

.chip i {
  width: 6px;
  height: 6px;
  flex-shrink: 0;
  border-radius: 50%;
  background: var(--border-hover);
}

.chip.is-active {
  border-color: rgba(16, 185, 129, 0.55);
  background: rgba(16, 185, 129, 0.14);
  color: var(--text);
  font-weight: 600;
}

.chip.is-active i {
  background: #10b981;
  box-shadow: 0 0 0 3px rgba(16, 185, 129, 0.2);
}

.chip.is-standby i {
  background: rgba(16, 185, 129, 0.55);
}

.chip.is-failed {
  opacity: 0.55;
  text-decoration: line-through;
}

.chip.is-failed i {
  background: #ef4444;
}

.chip.is-skipped {
  opacity: 0.7;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}
</style>

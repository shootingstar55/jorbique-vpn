<!-- Copyright (c) 2026 Mancala Gaming. All rights reserved. -->
<script setup>
/** The stack cards themselves, shared by the Windows modal and the macOS panel. */
import { STACK_OPTIONS } from '../lib/stacks.js';

defineProps({
  /** The stack in force, marked as such. Empty when the caller does not know it. */
  current: { type: String, default: '' },
});

const emit = defineEmits(['pick']);
</script>

<template>
  <div class="stacks">
    <button
      v-for="option in STACK_OPTIONS"
      :key="option.mode"
      type="button"
      class="stacks__item"
      :class="`stacks__item--${option.mode}`"
      @click="emit('pick', option.mode)"
    >
      <span class="stacks__badge">{{ option.title.charAt(0) }}</span>
      <span class="stacks__text">
        <span class="stacks__title">{{ option.title }}</span>
        <span class="stacks__subtitle">{{ option.subtitle }}</span>
      </span>
      <span v-if="option.mode === current" class="stacks__current">in use</span>
    </button>
  </div>
</template>

<style scoped>
.stacks {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.stacks__item {
  display: flex;
  align-items: center;
  gap: 14px;
  width: 100%;
  padding: 14px 16px;
  text-align: left;
  cursor: pointer;
  border-radius: 14px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-left: 3px solid transparent;
  transition: border-color 0.18s ease, background 0.18s ease, transform 0.18s ease;
}

.stacks__item--system { --tone: #10b981; }
.stacks__item--gvisor { --tone: #f59e0b; }
.stacks__item--mixed { --tone: #38bdf8; }

.stacks__item:hover {
  background: var(--surface-hover);
  border-color: var(--border-hover);
  border-left-color: var(--tone);
  transform: translateX(2px);
}

.stacks__badge {
  display: grid;
  place-items: center;
  width: 38px;
  height: 38px;
  flex-shrink: 0;
  border-radius: 11px;
  font-weight: 700;
  font-size: 15px;
  color: var(--tone);
  background: color-mix(in srgb, var(--tone) 14%, transparent);
  border: 1px solid color-mix(in srgb, var(--tone) 28%, transparent);
}

.stacks__text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.stacks__title {
  font-weight: 600;
  color: var(--text);
}

.stacks__subtitle {
  font-size: 13px;
  color: var(--text-muted);
}

.stacks__current {
  margin-left: auto;
  font-size: 12px;
  color: var(--tone);
}
</style>

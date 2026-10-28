<script setup>
import { computed } from 'vue';
import { NButton, NIcon, NTooltip } from 'naive-ui';
import { setThemeMode, themeMode } from '../lib/theme.js';
import AppIcon from './AppIcon.vue';

// The tooltip is the root, so a class from the parent would land on it and not on the button.
defineOptions({ inheritAttrs: false });
const emit = defineEmits(['change']);

const MODES = {
  light: { icon: 'sun', label: 'Тема: светлая' },
  dark: { icon: 'moon', label: 'Тема: тёмная' },
};

const current = computed(() => MODES[themeMode.value] || MODES.dark);

function next() {
  const mode = themeMode.value === 'dark' ? 'light' : 'dark';
  setThemeMode(mode);
  emit('change', mode);
}
</script>

<template>
  <NTooltip placement="bottom" :delay="300">
    <template #trigger>
      <NButton v-bind="$attrs" class="theme-switch" circle quaternary size="small" :aria-label="current.label" @click="next">
        <template #icon><NIcon :size="18"><AppIcon :name="current.icon" /></NIcon></template>
      </NButton>
    </template>
    {{ current.label }}
  </NTooltip>
</template>

<style scoped>
.theme-switch {
  flex-shrink: 0;
  color: var(--text-muted);
}
</style>

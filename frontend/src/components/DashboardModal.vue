<!-- Copyright (c) 2026 Mancala Gaming. All rights reserved. -->
<script setup>
import { NModal } from 'naive-ui';
import DashboardView from './DashboardView.vue';

defineProps({ visible: { type: Boolean, default: false } });
const emit = defineEmits(['update:visible']);
</script>

<template>
  <NModal
    :show="visible"
    preset="card"
    class="dialog dialog--dashboard"
    :bordered="false"
    :closable="false"
    @update:show="$event || emit('update:visible', false)"
  >
    <DashboardView v-if="visible" @close="emit('update:visible', false)" />
  </NModal>
</template>

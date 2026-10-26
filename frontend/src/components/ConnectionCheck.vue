<script setup>
import { computed } from 'vue';
const props = defineProps({ progress: { type: Object, default: () => ({}) } });
const phase = computed(() => props.progress?.phase || 'preparing');
const title = computed(() => ({
  preparing: 'Готовим проверку подключений',
  preferred: 'Проверяем основное подключение',
  alternatives: 'Ищем самое быстрое рабочее подключение',
  starting: 'Подключаем VPN',
}[phase.value] || 'Готовим подключение'));
const note = computed(() => {
  const p = props.progress || {};
  if (phase.value === 'alternatives' && p.total > 0) return `Проверено подключений: ${p.completed || 0} из ${p.total}`;
  if (phase.value === 'preferred') return 'Если оно работает, можно пользоваться.';
  if (phase.value === 'starting') return 'Подключение выбрано. Защищаем трафик…';
  return 'Перед подключением проверяем реальную передачу данных.';
});
const steps = computed(() => [
  { label: 'Проверка', active: ['preparing', 'preferred'].includes(phase.value), done: ['alternatives', 'starting'].includes(phase.value) },
  { label: 'Сравнение', active: phase.value === 'alternatives', done: phase.value === 'starting' },
  { label: 'Подключение', active: phase.value === 'starting', done: false },
]);
</script>

<template>
  <div class="connection-check" role="status" aria-live="polite" aria-atomic="true">
    <div class="check-title"><span class="check-scan" aria-hidden="true" />{{ title }}</div>
    <div class="check-note">{{ note }}</div>
    <div class="check-steps" aria-hidden="true">
      <span v-for="step in steps" :key="step.label" :class="{ active: step.active, done: step.done }">
        <i />{{ step.label }}
      </span>
    </div>
  </div>
</template>

<style scoped>
.connection-check { width: 100%; max-width: 380px; margin: 10px auto 0; padding: 12px 14px; box-sizing: border-box; border: 1px solid rgba(52,211,153,.22); border-radius: 12px; background: var(--surface-sunken); text-align: center; }
.check-title { display: flex; align-items: center; justify-content: center; gap: 8px; font-size: 12px; font-weight: 600; color: var(--text); }
.check-scan { width: 12px; height: 12px; flex-shrink: 0; border: 2px solid rgba(52,211,153,.2); border-top-color: #34d399; border-radius: 50%; animation: scan 1s linear infinite; }
.check-note { color: var(--text-muted); font-size: 11px; margin-top: 5px; }
.check-steps { display: flex; justify-content: center; gap: 22px; margin-top: 10px; }
.check-steps span { display: flex; align-items: center; gap: 5px; color: var(--text-dim); font-size: 10px; }
.check-steps i { width: 5px; height: 5px; border-radius: 50%; background: var(--border); }
.check-steps .active { color: var(--text); }
.check-steps .active i { background: #34d399; animation: pulse 1.4s ease-in-out infinite; }
.check-steps .done i { background: #34d399; }
@keyframes scan { to { transform: rotate(360deg); } }
@keyframes pulse { 50% { box-shadow: 0 0 0 4px rgba(52,211,153,.15); } }
@media (prefers-reduced-motion: reduce) { .check-scan, .check-steps .active i { animation: none; } }
</style>

<!-- Copyright (c) 2026 Mancala Gaming. All rights reserved. -->
<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import {
  NButton,
  NCard,
  NEmpty,
  NIcon,
  NSpin,
  NTabPane,
  NTabs,
} from 'naive-ui';
import AppIcon from './AppIcon.vue';
import { dashboardAPI } from '../lib/dashboard-api.js';
import { getErrMsg } from '../lib/errors.js';
import { formatBytes } from '../lib/format.js';
import { labels } from '../lib/labels.js';

defineProps({ standalone: { type: Boolean, default: false } });
const emit = defineEmits(['close']);

const snapshot = ref(null);
const error = ref('');
const loading = ref(true);
const action = ref('');
const activeTab = ref('overview');
const rates = ref({ uplink: 0, downlink: 0 });
let previousTotals = null;
let pollTimer;
let actionRefreshTimer;
let refreshing = false;
let disposed = false;

const status = computed(() => snapshot.value?.status || {});
const uploadRate = computed(() => formatBytes(rates.value.uplink) + '/s');
const downloadRate = computed(() => formatBytes(rates.value.downlink) + '/s');

async function refresh() {
  if (disposed || refreshing) return;
  refreshing = true;
  try {
    const next = await dashboardAPI.snapshot();
    if (disposed) return;
    const now = Date.now();
    if (previousTotals) {
      const seconds = Math.max(0.25, (now - previousTotals.at) / 1000);
      rates.value = {
        uplink: Math.max(0, (Number(next.status?.uplinkTotal) - previousTotals.uplink) / seconds),
        downlink: Math.max(0, (Number(next.status?.downlinkTotal) - previousTotals.downlink) / seconds),
      };
    } else {
      rates.value = {
        uplink: Math.max(0, Number(next.status?.uplink) || 0),
        downlink: Math.max(0, Number(next.status?.downlink) || 0),
      };
    }
    previousTotals = {
      at: now,
      uplink: Number(next.status?.uplinkTotal) || 0,
      downlink: Number(next.status?.downlinkTotal) || 0,
    };
    snapshot.value = next;
    error.value = '';
  } catch (e) {
    if (!disposed) error.value = getErrMsg(e, 'sing-box API is unavailable');
  } finally {
    if (!disposed) loading.value = false;
    refreshing = false;
  }
}

async function run(key, operation) {
  if (action.value) return;
  action.value = key;
  try {
    await operation();
    if (!disposed) actionRefreshTimer = setTimeout(refresh, 250);
  } catch (e) {
    error.value = getErrMsg(e, 'Action failed');
  } finally {
    action.value = '';
  }
}

function choose(group, item) {
  if (!group.selectable || group.selected === item.tag) return;
  run(`select:${group.tag}:${item.tag}`, () => dashboardAPI.select(group.tag, item.tag));
}

function testGroup(group) {
  run(`test:${group.tag}`, () => dashboardAPI.urlTest(group.tag));
}

onMounted(() => {
  refresh();
  pollTimer = setInterval(refresh, 1000);
});
onBeforeUnmount(() => {
  disposed = true;
  clearInterval(pollTimer);
  clearTimeout(actionRefreshTimer);
});
</script>

<template>
  <div class="dashboard" :class="{ 'dashboard--standalone': standalone }">
    <header class="dashboard__header">
      <div>
        <div class="eyebrow"><span class="live-dot" /> LIVE CONTROL</div>
        <h1>Jorbique VPN Dashboard</h1>
        <p>sing-box {{ snapshot?.version || '1.14' }}</p>
      </div>
      <NButton v-if="!standalone" quaternary circle aria-label="Close dashboard" @click="emit('close')">
        <template #icon><NIcon><AppIcon name="close" /></NIcon></template>
      </NButton>
    </header>

    <div v-if="error" class="dashboard__error">
      <span>{{ error }}</span>
      <NButton size="small" tertiary @click="refresh">{{ labels.retry }}</NButton>
    </div>

    <NSpin :show="loading">
      <NTabs v-model:value="activeTab" type="segment" animated class="dashboard__tabs">
        <NTabPane name="overview" tab="Overview">
          <div class="metrics">
            <NCard class="metric metric--upload" :bordered="false">
              <span class="metric__label">{{ labels.upload }}</span>
              <strong>{{ uploadRate }}</strong>
              <small>{{ formatBytes(status.uplinkTotal) }} total</small>
            </NCard>
            <NCard class="metric metric--download" :bordered="false">
              <span class="metric__label">{{ labels.download }}</span>
              <strong>{{ downloadRate }}</strong>
              <small>{{ formatBytes(status.downlinkTotal) }} total</small>
            </NCard>
            <NCard class="metric" :bordered="false">
              <span class="metric__label">{{ labels.runtime }}</span>
              <div class="metric__rows">
                <span>{{ labels.memory }} <b>{{ formatBytes(status.memory) }}</b></span>
                <span>{{ labels.goroutines }} <b>{{ status.goroutines ?? '—' }}</b></span>
              </div>
            </NCard>
            <NCard class="metric" :bordered="false">
              <span class="metric__label">{{ labels.connections }}</span>
              <div class="metric__rows">
                <span>{{ labels.inbound }} <b>{{ status.connectionsIn ?? '—' }}</b></span>
                <span>{{ labels.outbound }} <b>{{ status.connectionsOut ?? '—' }}</b></span>
              </div>
            </NCard>
          </div>
        </NTabPane>

        <NTabPane name="groups" tab="Groups">
          <div v-if="snapshot?.groups?.length" class="groups">
            <section v-for="group in snapshot.groups" :key="group.tag" class="group">
              <div class="group__head">
                <div>
                  <strong>{{ group.tag }}</strong>
                  <span>{{ group.type }}</span>
                </div>
                <NButton
                  v-if="String(group.type).toLowerCase() === 'urltest'"
                  size="small"
                  tertiary
                  :loading="action === `test:${group.tag}`"
                  @click="testGroup(group)"
                >
                  <template #icon><NIcon><AppIcon name="refresh" /></NIcon></template>
                  Retest
                </NButton>
              </div>
              <div class="group__items">
                <button
                  v-for="item in group.items"
                  :key="item.tag"
                  type="button"
                  class="outbound"
                  :class="{
                    'outbound--selected': group.selected === item.tag,
                    'outbound--clickable': group.selectable,
                  }"
                  :disabled="!group.selectable || !!action"
                  @click="choose(group, item)"
                >
                  <strong>{{ item.tag }}</strong>
                  <span>{{ item.type || 'Outbound' }}</span>
                  <b :class="{ 'latency--bad': item.urlTestDelay <= 0 }">
                    {{ item.urlTestDelay > 0 ? `${item.urlTestDelay} ms` : 'Unavailable' }}
                  </b>
                </button>
              </div>
            </section>
          </div>
          <NEmpty v-else description="No selector or URLTest groups" />
        </NTabPane>

      </NTabs>
    </NSpin>
  </div>
</template>

<style scoped>
.dashboard { min-height: 560px; color: var(--text); }
.dashboard--standalone { min-height: 100vh; padding: 30px; max-width: 1180px; margin: 0 auto; }
.dashboard__header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 18px; }
.dashboard__header h1 { margin: 4px 0 0; font-size: 24px; letter-spacing: -.03em; color: var(--text); }
.dashboard__header p { margin: 3px 0 0; color: var(--text-dim); font-size: 12px; }
.eyebrow { color: #34d399; font-size: 10px; font-weight: 800; letter-spacing: .14em; }
.live-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  margin-right: 6px;
  border-radius: 50%;
  background: #10b981;
  box-shadow: 0 0 10px #10b981;
}
.dashboard__error {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
  padding: 10px 14px;
  border: 1px solid rgba(244,63,94,.35);
  border-radius: 12px;
  background: var(--surface);
  color: #f43f5e;
  font-size: 12px;
}
.dashboard__tabs :deep(.n-tabs-rail) { max-width: 430px; }
.metrics { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; padding-top: 14px; }
.metric {
  min-height: 154px;
  position: relative;
  overflow: hidden;
  border: 1px solid var(--border);
  background: linear-gradient(145deg, var(--surface), var(--surface-sunken));
}
.metric::after {
  content: '';
  position: absolute;
  width: 120px;
  height: 120px;
  right: -42px;
  bottom: -58px;
  border-radius: 50%;
  background: rgba(16,185,129,.1);
  filter: blur(4px);
}
.metric--upload::after { background: rgba(56,189,248,.12); }
.metric__label {
  display: block;
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: .08em;
  text-transform: uppercase;
}
.metric strong { display: inline-block; margin-top: 14px; font-size: 30px; color: var(--text); letter-spacing: -.04em; }
.metric small { margin-left: 8px; color: var(--text-dim); }
.metric__rows { display: grid; gap: 14px; margin-top: 19px; }
.metric__rows span {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  color: var(--text-muted);
  font-size: 13px;
}
.metric__rows b { color: var(--text); font-weight: 650; text-align: right; }
.groups { display: grid; gap: 12px; padding-top: 14px; }
.group { padding: 16px; border: 1px solid var(--border); border-radius: 18px; background: var(--surface); }
.group__head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 13px; }
.group__head strong { color: var(--text); }
.group__head span { margin-left: 8px; color: var(--text-dim); font-size: 12px; }
.group__items { display: grid; grid-template-columns: repeat(auto-fill, minmax(145px, 1fr)); gap: 9px; }
.outbound {
  min-width: 0;
  padding: 12px;
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 3px 8px;
  text-align: left;
  border: 1px solid transparent;
  border-radius: 12px;
  color: var(--text);
  background: var(--surface-sunken);
}
.outbound--clickable { cursor: pointer; }
.outbound--clickable:hover { border-color: rgba(52,211,153,.38); background: rgba(6,78,59,.2); }
.outbound--selected { border-color: #10b981; box-shadow: inset 0 0 0 1px rgba(16,185,129,.25); }
.outbound strong { overflow: hidden; text-overflow: ellipsis; }
.outbound span { grid-column: 1; color: var(--text-dim); font-size: 11px; }
.outbound b { grid-column: 2; grid-row: 2; color: #34d399; font-size: 11px; }
.outbound .latency--bad { color: #fb7185; }
@media (max-width: 760px) { .dashboard { min-height: 500px; } .dashboard--standalone { padding: 18px; } }
</style>

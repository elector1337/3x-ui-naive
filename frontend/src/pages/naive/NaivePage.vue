<script setup>
import { computed, ref } from 'vue';
import { useI18n } from 'vue-i18n';
import { Modal, message } from 'ant-design-vue';
import {
  PlusOutlined,
  PlayCircleOutlined,
  PauseCircleOutlined,
  ReloadOutlined,
  EditOutlined,
  FileTextOutlined,
  DeleteOutlined,
  CopyOutlined,
  CloudServerOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
} from '@ant-design/icons-vue';

import { theme as themeState, antdThemeConfig } from '@/composables/useTheme.js';
import { useMediaQuery } from '@/composables/useMediaQuery.js';
import { ClipboardManager, SizeFormatter } from '@/utils';
import AppSidebar from '@/components/AppSidebar.vue';
import CustomStatistic from '@/components/CustomStatistic.vue';
import NaiveFormModal from './NaiveFormModal.vue';
import { useNaive } from './useNaive.js';

const { t } = useI18n();

const {
  servers,
  statuses,
  loading,
  fetched,
  caddy,
  refresh,
  installCaddy,
  fetchLog,
  create,
  update,
  remove,
  start,
  stop,
  restart,
  resetTraffic,
} = useNaive();

const installing = ref(false);
const installLog = ref('');
async function onInstallCaddy() {
  installing.value = true;
  installLog.value = '';
  try {
    const res = await installCaddy((chunk) => { installLog.value += chunk + '\n'; });
    if (res.ok) message.success(t('pages.naive.toasts.caddyInstalled'));
    else message.error(res.err || t('pages.naive.toasts.caddyInstallFailed'));
  } catch (e) {
    message.error(String(e?.message || e));
  } finally {
    installing.value = false;
  }
}

const { isMobile } = useMediaQuery();

const basePath = window.X_UI_BASE_PATH || '';
const requestUri = window.location.pathname;

const formOpen = ref(false);
const formMode = ref('add');
const formServer = ref(null);

function onAdd() {
  formMode.value = 'add';
  formServer.value = null;
  formOpen.value = true;
}

function onEdit(s) {
  formMode.value = 'edit';
  formServer.value = { ...s };
  formOpen.value = true;
}

async function onSave(payload) {
  if (formMode.value === 'edit' && formServer.value?.id) {
    return update(formServer.value.id, payload);
  }
  return create(payload);
}

function onDelete(s) {
  Modal.confirm({
    title: t('pages.naive.deleteConfirmTitle', { name: s.remark || s.domain }),
    okText: t('delete'),
    okType: 'danger',
    cancelText: t('cancel'),
    onOk: async () => {
      const msg = await remove(s.id);
      if (msg?.success) message.success(t('pages.naive.toasts.deleted'));
    },
  });
}

async function onStart(s) {
  const msg = await start(s.id);
  if (msg?.success) message.success(t('pages.naive.toasts.started'));
}
async function onStop(s) {
  const msg = await stop(s.id);
  if (msg?.success) message.success(t('pages.naive.toasts.stopped'));
}
const logOpen = ref(false);
const logTarget = ref(null);
const logText = ref('');
const logLoading = ref(false);

async function onShowLog(srv) {
  logTarget.value = srv;
  logOpen.value = true;
  await reloadLog();
}

async function reloadLog() {
  if (!logTarget.value) return;
  logLoading.value = true;
  try {
    const res = await fetchLog(logTarget.value.id, 200);
    if (res?.success) {
      logText.value = res.obj || '';
    } else {
      logText.value = res?.msg || t('pages.naive.logError');
    }
  } finally {
    logLoading.value = false;
  }
}

async function onRestart(s) {
  const msg = await restart(s.id);
  if (msg?.success) message.success(t('pages.naive.toasts.restarted'));
}

async function onResetTraffic(s) {
  const msg = await resetTraffic(s.id);
  if (msg?.success) message.success(t('pages.naive.toasts.trafficReset'));
}

function clientUrl(s) {
  const user = encodeURIComponent(s.authUser);
  const pass = encodeURIComponent(s.authPass);
  return `naive+https://${user}:${pass}@${s.domain}:${s.port}`;
}

async function onCopy(s) {
  await ClipboardManager.write(clientUrl(s));
  message.success(t('copied'));
}

function isRunning(id) {
  return !!statuses.value[id]?.running;
}

const totals = computed(() => {
  const list = servers.value;
  let running = 0;
  for (const s of list) if (statuses.value[s.id]?.running) running += 1;
  return {
    total: list.length,
    running,
    stopped: Math.max(0, list.length - running),
  };
});

const columns = computed(() => [
  { key: 'remark', title: t('pages.naive.fields.remark'), dataIndex: 'remark' },
  { key: 'domain', title: t('pages.naive.fields.domain') },
  { key: 'enable', title: t('pages.naive.fields.enable'), width: 110 },
  { key: 'status', title: t('pages.naive.statusCol'), width: 120 },
  { key: 'traffic', title: t('pages.naive.trafficCol'), width: 150 },
  { key: 'actions', title: '', width: 340, align: 'right' },
]);
</script>

<template>
  <a-config-provider :theme="antdThemeConfig">
    <a-layout class="naive-page" :class="{ 'is-dark': themeState.isDark, 'is-ultra': themeState.isUltra }">
      <AppSidebar :base-path="basePath" :request-uri="requestUri" />

      <a-layout class="content-shell">
        <a-layout-content id="content-layout" class="content-area">
          <a-spin :spinning="!fetched" :delay="200" tip="Loading…" size="large">
            <div v-if="!fetched" class="loading-spacer" />

            <a-row v-else :gutter="[isMobile ? 8 : 16, isMobile ? 8 : 12]">
              <!-- caddy install banner -->
              <a-col v-if="!caddy.installed" :span="24">
                <a-alert
                  type="warning"
                  show-icon
                  :message="t('pages.naive.caddy.missingTitle')"
                  :description="caddy.goPresent ? t('pages.naive.caddy.canInstall') : t('pages.naive.caddy.noGo')"
                >
                  <template #action>
                    <a-button
                      v-if="caddy.goPresent"
                      size="small"
                      type="primary"
                      :loading="installing"
                      @click="onInstallCaddy"
                    >
                      {{ installing ? t('pages.naive.caddy.installing') : t('pages.naive.caddy.install') }}
                    </a-button>
                  </template>
                </a-alert>
              </a-col>
              <a-col v-if="installing || installLog" :span="24">
                <a-card size="small">
                  <pre class="install-log">{{ installLog || '...' }}</pre>
                </a-card>
              </a-col>

              <a-col v-if="caddy.installed" :span="24">
                <a-alert
                  type="success"
                  show-icon
                  :message="t('pages.naive.caddy.installedTitle', { v: caddy.version || '?' })"
                  :description="`${caddy.source}: ${caddy.path}`"
                  closable
                />
              </a-col>

              <!-- summary -->
              <a-col :span="24">
                <a-card size="small" hoverable class="summary-card">
                  <a-row :gutter="[16, isMobile ? 16 : 12]">
                    <a-col :xs="8" :sm="8" :md="8">
                      <CustomStatistic :title="t('pages.naive.totals.total')" :value="String(totals.total)">
                        <template #prefix>
                          <CloudServerOutlined />
                        </template>
                      </CustomStatistic>
                    </a-col>
                    <a-col :xs="8" :sm="8" :md="8">
                      <CustomStatistic :title="t('pages.naive.totals.running')" :value="String(totals.running)">
                        <template #prefix>
                          <CheckCircleOutlined style="color: #52c41a" />
                        </template>
                      </CustomStatistic>
                    </a-col>
                    <a-col :xs="8" :sm="8" :md="8">
                      <CustomStatistic :title="t('pages.naive.totals.stopped')" :value="String(totals.stopped)">
                        <template #prefix>
                          <CloseCircleOutlined style="color: #ff4d4f" />
                        </template>
                      </CustomStatistic>
                    </a-col>
                  </a-row>
                </a-card>
              </a-col>

              <!-- table -->
              <a-col :span="24">
                <a-card size="small" hoverable class="list-card">
                  <template #title>
                    <span>{{ t('pages.naive.title') }}</span>
                  </template>
                  <template #extra>
                    <a-space>
                      <a-button :loading="loading" @click="refresh">
                        <template #icon><ReloadOutlined /></template>
                        {{ t('refresh') }}
                      </a-button>
                      <a-button type="primary" @click="onAdd">
                        <template #icon><PlusOutlined /></template>
                        {{ t('pages.naive.add') }}
                      </a-button>
                    </a-space>
                  </template>

                  <!-- mobile card stack -->
                  <div v-if="isMobile" class="mobile-cards">
                    <div v-if="servers.length === 0" class="card-empty">
                      <a-empty :description="t('pages.naive.empty')" />
                    </div>
                    <div v-for="srv in servers" :key="srv.id" class="srv-card">
                      <div class="srv-head">
                        <div class="srv-title">{{ srv.remark || `naive-${srv.id}` }}</div>
                        <a-tag
                          v-if="isRunning(srv.id) && statuses[srv.id]?.listening && statuses[srv.id]?.responding"
                          color="success"
                        >{{ t('pages.naive.running') }}</a-tag>
                        <a-tag
                          v-else-if="isRunning(srv.id) && statuses[srv.id]?.listening"
                          color="warning"
                        >{{ t('pages.naive.unresponsive') }}</a-tag>
                        <a-tag v-else-if="isRunning(srv.id)" color="processing">{{ t('pages.naive.starting') }}</a-tag>
                        <a-tag v-else color="default">{{ t('pages.naive.stopped') }}</a-tag>
                      </div>
                      <div class="srv-row">
                        <span class="srv-label">Endpoint</span>
                        <code>{{ srv.domain }}:{{ srv.port }}</code>
                      </div>
                      <div class="srv-row">
                        <span class="srv-label">{{ t('pages.naive.fields.enable') }}</span>
                        <a-tag :color="srv.enable ? 'green' : 'default'" :bordered="false">
                          {{ srv.enable ? t('pages.naive.enabled') : t('pages.naive.disabled') }}
                        </a-tag>
                      </div>
                      <a-space :size="6" wrap class="srv-actions">
                        <a-button size="small" @click="onCopy(srv)">
                          <template #icon><CopyOutlined /></template>
                        </a-button>
                        <a-button
                          v-if="!isRunning(srv.id)"
                          size="small"
                          type="primary"
                          @click="onStart(srv)"
                        >
                          <template #icon><PlayCircleOutlined /></template>
                          {{ t('pages.naive.start') }}
                        </a-button>
                        <a-button v-else size="small" danger @click="onStop(srv)">
                          <template #icon><PauseCircleOutlined /></template>
                          {{ t('pages.naive.stop') }}
                        </a-button>
                        <a-button size="small" :disabled="!isRunning(srv.id)" @click="onRestart(srv)">
                          <template #icon><ReloadOutlined /></template>
                        </a-button>
                        <a-button size="small" @click="onShowLog(srv)">
                          <template #icon><FileTextOutlined /></template>
                        </a-button>
                        <a-button size="small" @click="onEdit(srv)">
                          <template #icon><EditOutlined /></template>
                        </a-button>
                        <a-button size="small" danger @click="onDelete(srv)">
                          <template #icon><DeleteOutlined /></template>
                        </a-button>
                      </a-space>
                    </div>
                  </div>

                  <!-- desktop table -->
                  <a-table
                    v-else
                    :columns="columns"
                    :data-source="servers"
                    :pagination="false"
                    :row-key="(r) => r.id"
                    :size="isMobile ? 'small' : 'middle'"
                    :scroll="{ x: 'max-content' }"
                  >
                    <template #bodyCell="{ column, record }">
                      <template v-if="column.key === 'remark'">
                        {{ record.remark || `naive-${record.id}` }}
                      </template>

                      <template v-else-if="column.key === 'domain'">
                        <code>{{ record.domain }}:{{ record.port }}</code>
                      </template>

                      <template v-else-if="column.key === 'enable'">
                        <a-tag :color="record.enable ? 'green' : 'default'">
                          {{ record.enable ? t('pages.naive.enabled') : t('pages.naive.disabled') }}
                        </a-tag>
                      </template>

                      <template v-else-if="column.key === 'status'">
                        <a-tag
                          v-if="isRunning(record.id) && statuses[record.id]?.listening && statuses[record.id]?.responding"
                          color="success"
                        >
                          {{ t('pages.naive.running') }}
                        </a-tag>
                        <a-tag
                          v-else-if="isRunning(record.id) && statuses[record.id]?.listening"
                          color="warning"
                        >
                          {{ t('pages.naive.unresponsive') }}
                        </a-tag>
                        <a-tag v-else-if="isRunning(record.id)" color="processing">
                          {{ t('pages.naive.starting') }}
                        </a-tag>
                        <a-tag v-else color="default">{{ t('pages.naive.stopped') }}</a-tag>
                      </template>

                      <template v-else-if="column.key === 'traffic'">
                        <a-tooltip :title="t('pages.naive.trafficHint')">
                          <span>↑ {{ SizeFormatter.sizeFormat(record.up || 0) }} / ↓ {{ SizeFormatter.sizeFormat(record.down || 0) }}</span>
                        </a-tooltip>
                      </template>

                      <template v-else-if="column.key === 'actions'">
                        <a-space :size="4" wrap>
                          <a-tooltip :title="t('pages.naive.copyUrl')">
                            <a-button size="small" @click="onCopy(record)">
                              <template #icon><CopyOutlined /></template>
                            </a-button>
                          </a-tooltip>
                          <a-button
                            v-if="!isRunning(record.id)"
                            size="small"
                            type="primary"
                            @click="onStart(record)"
                          >
                            <template #icon><PlayCircleOutlined /></template>
                            {{ t('pages.naive.start') }}
                          </a-button>
                          <a-button v-else size="small" danger @click="onStop(record)">
                            <template #icon><PauseCircleOutlined /></template>
                            {{ t('pages.naive.stop') }}
                          </a-button>
                          <a-tooltip :title="t('pages.naive.restart')">
                            <a-button size="small" :disabled="!isRunning(record.id)" @click="onRestart(record)">
                              <template #icon><ReloadOutlined /></template>
                            </a-button>
                          </a-tooltip>
                          <a-tooltip :title="t('pages.naive.viewLog')">
                            <a-button size="small" @click="onShowLog(record)">
                              <template #icon><FileTextOutlined /></template>
                            </a-button>
                          </a-tooltip>
                          <a-button size="small" @click="onEdit(record)">
                            <template #icon><EditOutlined /></template>
                          </a-button>
                          <a-tooltip :title="t('pages.naive.resetTraffic')">
                            <a-button size="small" @click="onResetTraffic(record)">
                              <template #icon><ReloadOutlined /></template>
                            </a-button>
                          </a-tooltip>
                          <a-button size="small" danger @click="onDelete(record)">
                            <template #icon><DeleteOutlined /></template>
                          </a-button>
                        </a-space>
                      </template>
                    </template>

                    <template #emptyText>
                      <a-empty :description="t('pages.naive.empty')" />
                    </template>
                  </a-table>
                </a-card>
              </a-col>
            </a-row>
          </a-spin>
        </a-layout-content>
      </a-layout>

      <NaiveFormModal
        v-model:open="formOpen"
        :mode="formMode"
        :server="formServer"
        @save="onSave"
      />

      <a-modal
        v-model:open="logOpen"
        :title="t('pages.naive.logTitle', { name: logTarget?.remark || `naive-${logTarget?.id}` })"
        width="780px"
        :footer="null"
      >
        <a-space style="margin-bottom: 8px;">
          <a-button :loading="logLoading" size="small" @click="reloadLog">
            <template #icon><ReloadOutlined /></template>
            {{ t('refresh') }}
          </a-button>
          <span class="log-hint">{{ t('pages.naive.logHint') }}</span>
        </a-space>
        <pre class="log-view">{{ logText || t('pages.naive.logEmpty') }}</pre>
      </a-modal>
    </a-layout>
  </a-config-provider>
</template>

<style scoped>
.naive-page {
  --bg-page: #e6e8ec;
  --bg-card: #ffffff;

  min-height: 100vh;
  background: var(--bg-page);
}

.naive-page.is-dark {
  --bg-page: #1e1e1e;
  --bg-card: #252526;
}

.naive-page.is-dark.is-ultra {
  --bg-page: #050505;
  --bg-card: #0c0e12;
}

.naive-page :deep(.ant-layout),
.naive-page :deep(.ant-layout-content) {
  background: transparent;
}

.content-shell {
  background: transparent;
}

.content-area {
  padding: 24px;
}

@media (max-width: 768px) {
  .content-area {
    padding: 8px;
  }
}

.loading-spacer {
  min-height: calc(100vh - 120px);
}

.summary-card {
  padding: 16px;
}

@media (max-width: 768px) {
  .summary-card {
    padding: 8px;
  }
}

.list-card code {
  font-size: 12px;
}

.install-log {
  max-height: 240px;
  overflow: auto;
  font-size: 11px;
  white-space: pre-wrap;
  margin: 0;
}

/* mobile card stack — mirrors the nodes/inbounds responsive pattern */
.mobile-cards {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.card-empty {
  padding: 16px 0;
}

.srv-card {
  border: 1px solid rgba(0, 0, 0, 0.06);
  border-radius: 6px;
  padding: 10px 12px;
  background: var(--bg-card);
}

.naive-page.is-dark .srv-card {
  border-color: rgba(255, 255, 255, 0.08);
}

.srv-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}

.srv-title {
  font-weight: 600;
  font-size: 14px;
  margin-right: 8px;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.srv-row {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  padding: 2px 0;
  gap: 12px;
}

.srv-row code {
  font-size: 12px;
  color: inherit;
}

.srv-label {
  opacity: 0.65;
}

.srv-actions {
  margin-top: 8px;
}

.log-view {
  max-height: 480px;
  overflow: auto;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  line-height: 1.4;
  background: rgba(0, 0, 0, 0.04);
  padding: 10px 12px;
  border-radius: 4px;
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
}

.naive-page.is-dark .log-view {
  background: rgba(255, 255, 255, 0.06);
}

.log-hint {
  font-size: 11px;
  opacity: 0.6;
}
</style>

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
  DeleteOutlined,
  CopyOutlined,
  CloudServerOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
} from '@ant-design/icons-vue';

import { theme as themeState, antdThemeConfig } from '@/composables/useTheme.js';
import { useMediaQuery } from '@/composables/useMediaQuery.js';
import { ClipboardManager } from '@/utils';
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
  refresh,
  create,
  update,
  remove,
  start,
  stop,
  restart,
} = useNaive();

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
async function onRestart(s) {
  const msg = await restart(s.id);
  if (msg?.success) message.success(t('pages.naive.toasts.restarted'));
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
  { key: 'actions', title: '', width: 300, align: 'right' },
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

                  <a-table
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
                        <a-tag v-if="isRunning(record.id)" color="processing">
                          {{ t('pages.naive.running') }}
                        </a-tag>
                        <a-tag v-else color="default">{{ t('pages.naive.stopped') }}</a-tag>
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
                          <a-button size="small" @click="onEdit(record)">
                            <template #icon><EditOutlined /></template>
                          </a-button>
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
</style>

<script setup>
import { ref } from 'vue';
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
} from '@ant-design/icons-vue';

import { theme as themeState, antdThemeConfig } from '@/composables/useTheme.js';
import { ClipboardManager } from '@/utils';
import AppSidebar from '@/components/AppSidebar.vue';
import NaiveFormModal from './NaiveFormModal.vue';
import { useNaive } from './useNaive.js';

const { t } = useI18n();

const {
  servers,
  statuses,
  fetched,
  refresh,
  refreshStatuses,
  create,
  update,
  remove,
  start,
  stop,
  restart,
} = useNaive();

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

const columns = [
  { key: 'remark', title: 'Name', dataIndex: 'remark' },
  { key: 'domain', title: 'Endpoint' },
  { key: 'enable', title: 'Enabled', dataIndex: 'enable', width: 90 },
  { key: 'status', title: 'Status', width: 110 },
  { key: 'actions', title: 'Actions', width: 280, align: 'right' },
];
</script>

<template>
  <a-config-provider :theme="antdThemeConfig">
    <a-layout class="naive-page" :class="{ 'is-dark': themeState.isDark, 'is-ultra': themeState.isUltra }">
      <AppSidebar :base-path="basePath" :request-uri="requestUri" />

      <a-layout class="content-shell">
        <a-layout-content class="content-area" style="padding: 16px;">
          <a-spin :spinning="!fetched" :delay="200" tip="Loading…" size="large">
            <div v-if="!fetched" style="height: 200px" />

            <a-card v-else size="small">
              <template #title>
                <span>{{ t('pages.naive.title') }}</span>
              </template>
              <template #extra>
                <a-space>
                  <a-button @click="refresh">
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
                size="middle"
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
                    <a-space wrap>
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
                      <a-button size="small" :disabled="!isRunning(record.id)" @click="onRestart(record)">
                        <template #icon><ReloadOutlined /></template>
                      </a-button>
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
          </a-spin>

          <NaiveFormModal
            v-model:open="formOpen"
            :mode="formMode"
            :server="formServer"
            @save="onSave"
          />
        </a-layout-content>
      </a-layout>
    </a-layout>
  </a-config-provider>
</template>

<style scoped>
.naive-page { min-height: 100vh; }
.content-shell { background: transparent; }
.content-area code { font-size: 12px; }
</style>

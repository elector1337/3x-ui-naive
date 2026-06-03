<script setup>
import { computed, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { message } from 'ant-design-vue';
import dayjs from 'dayjs';
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons-vue';
import { HttpUtil } from '@/utils';

const props = defineProps({
  open: { type: Boolean, default: false },
  mode: { type: String, default: 'add' },
  server: { type: Object, default: null },
});
const emit = defineEmits(['update:open', 'save']);

const { t } = useI18n();

function blank() {
  return {
    remark: '',
    enable: false,
    listen: '',
    port: 443,
    domain: '',
    useAcme: false,
    acmeEmail: '',
    certFile: '',
    keyFile: '',
    authUser: '',
    authPass: '',
    padding: true,
    logLevel: 'WARN',
    extraArgs: '',
    useRawConfig: false,
    rawConfig: '',
    total: 0,
    expiryTime: 0,
    trafficReset: 'never',
    users: [],
  };
}

const form = ref(blank());
const saving = ref(false);

function addUser() {
  if (!Array.isArray(form.value.users)) form.value.users = [];
  form.value.users.push({ username: '', password: '', enable: true });
}

function removeUser(idx) {
  form.value.users.splice(idx, 1);
}

const GB = 1024 * 1024 * 1024;

// Quota is stored in bytes but entered in GB. 0 = unlimited.
const totalGB = computed({
  get: () => (form.value.total > 0 ? +(form.value.total / GB).toFixed(2) : 0),
  set: (v) => { form.value.total = v > 0 ? Math.round(v * GB) : 0; },
});

// Expiry is stored as an absolute ms timestamp but edited as a date.
// 0 = never.
const expiryDate = computed({
  get: () => (form.value.expiryTime > 0 ? dayjs(form.value.expiryTime) : null),
  set: (v) => { form.value.expiryTime = v ? v.valueOf() : 0; },
});

watch(
  () => props.open,
  (v) => {
    if (v) form.value = props.server ? { ...blank(), ...props.server } : blank();
  },
);

const title = computed(() =>
  props.mode === 'edit' ? t('pages.naive.editTitle') : t('pages.naive.addTitle'),
);

function close() {
  emit('update:open', false);
}

function validate() {
  const f = form.value;
  if (f.useRawConfig) {
    if (!f.rawConfig.trim()) return t('pages.naive.errRawEmpty');
    return null;
  }
  if (!f.domain.trim()) return t('pages.naive.errDomain');
  if (!f.port || f.port < 1 || f.port > 65535) return t('pages.naive.errPort');
  if (!f.authUser.trim() || !f.authPass.trim()) return t('pages.naive.errAuth');
  if (f.useAcme) {
    if (!f.acmeEmail.trim()) return t('pages.naive.errAcmeEmail');
  } else if (!f.certFile.trim() || !f.keyFile.trim()) {
    return t('pages.naive.errCert');
  }
  return null;
}

// fill the raw editor with the Caddyfile we'd generate from the current form
async function prefillRaw() {
  const msg = await HttpUtil.post('/panel/api/naive/preview', { ...form.value, useRawConfig: false });
  if (msg?.success && msg.obj) {
    form.value.rawConfig = msg.obj;
  }
}

const validating = ref(false);
async function runValidate() {
  if (!form.value.rawConfig.trim()) {
    message.warning(t('pages.naive.errRawEmpty'));
    return;
  }
  validating.value = true;
  try {
    const msg = await HttpUtil.post('/panel/api/naive/validate', { text: form.value.rawConfig });
    if (msg?.success) message.success(t('pages.naive.rawValid'));
    else message.error(msg?.msg || t('pages.naive.rawInvalid'));
  } finally {
    validating.value = false;
  }
}

function onToggleRaw(v) {
  form.value.useRawConfig = v;
  if (v && !form.value.rawConfig.trim()) prefillRaw();
}

async function save() {
  const err = validate();
  if (err) {
    message.error(err);
    return;
  }
  saving.value = true;
  try {
    const msg = await emit('save', { ...form.value });
    if (msg && msg.then) await msg;
    close();
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <a-modal
    :open="open"
    :title="title"
    :confirm-loading="saving"
    :ok-text="t('save')"
    :cancel-text="t('cancel')"
    width="640px"
    @ok="save"
    @cancel="close"
  >
    <a-form layout="vertical">
      <a-row :gutter="12">
        <a-col :span="12">
          <a-form-item :label="t('pages.naive.fields.remark')">
            <a-input v-model:value="form.remark" />
          </a-form-item>
        </a-col>
        <a-col :span="6">
          <a-form-item :label="t('pages.naive.fields.enable')">
            <a-switch v-model:checked="form.enable" />
          </a-form-item>
        </a-col>
        <a-col :span="6">
          <a-form-item :label="t('pages.naive.fields.advanced')">
            <a-switch
              :checked="form.useRawConfig"
              @change="onToggleRaw"
            />
          </a-form-item>
        </a-col>
      </a-row>

      <!-- Advanced (raw Caddyfile) mode -->
      <template v-if="form.useRawConfig">
        <a-alert
          type="info"
          show-icon
          :message="t('pages.naive.rawHint')"
          style="margin-bottom: 12px;"
        />
        <a-form-item :label="t('pages.naive.fields.rawConfig')" required>
          <a-textarea
            v-model:value="form.rawConfig"
            :auto-size="{ minRows: 12, maxRows: 24 }"
            class="raw-editor"
            spellcheck="false"
          />
        </a-form-item>
        <a-space style="margin-bottom: 12px;">
          <a-button :loading="validating" @click="runValidate">{{ t('pages.naive.validate') }}</a-button>
          <a-button @click="prefillRaw">{{ t('pages.naive.regenerate') }}</a-button>
        </a-space>
      </template>

      <!-- Simple form mode -->
      <template v-else>

      <a-row :gutter="12">
        <a-col :span="14">
          <a-form-item :label="t('pages.naive.fields.domain')" required>
            <a-input v-model:value="form.domain" placeholder="example.com" />
          </a-form-item>
        </a-col>
        <a-col :span="6">
          <a-form-item :label="t('pages.naive.fields.port')" required>
            <a-input-number v-model:value="form.port" :min="1" :max="65535" style="width: 100%" />
          </a-form-item>
        </a-col>
        <a-col :span="4">
          <a-form-item :label="t('pages.naive.fields.listen')">
            <a-input v-model:value="form.listen" placeholder="all" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-row :gutter="12">
        <a-col :span="12">
          <a-form-item :label="t('pages.naive.fields.authUser')" required>
            <a-input v-model:value="form.authUser" />
          </a-form-item>
        </a-col>
        <a-col :span="12">
          <a-form-item :label="t('pages.naive.fields.authPass')" required>
            <a-input-password v-model:value="form.authPass" />
          </a-form-item>
        </a-col>
      </a-row>

      <a-row :gutter="12">
        <a-col :span="10">
          <a-form-item :label="t('pages.naive.fields.useAcme')">
            <a-switch v-model:checked="form.useAcme" />
          </a-form-item>
        </a-col>
        <a-col :span="14">
          <a-form-item v-if="form.useAcme" :label="t('pages.naive.fields.acmeEmail')" required>
            <a-input v-model:value="form.acmeEmail" placeholder="me@example.com" />
          </a-form-item>
        </a-col>
      </a-row>

      <template v-if="!form.useAcme">
        <a-form-item :label="t('pages.naive.fields.certFile')" required>
          <a-input v-model:value="form.certFile" placeholder="/etc/letsencrypt/live/example.com/fullchain.pem" />
        </a-form-item>
        <a-form-item :label="t('pages.naive.fields.keyFile')" required>
          <a-input v-model:value="form.keyFile" placeholder="/etc/letsencrypt/live/example.com/privkey.pem" />
        </a-form-item>
      </template>

      <a-row :gutter="12">
        <a-col :span="8">
          <a-form-item :label="t('pages.naive.fields.padding')">
            <a-switch v-model:checked="form.padding" />
          </a-form-item>
        </a-col>
        <a-col :span="8">
          <a-form-item :label="t('pages.naive.fields.logLevel')">
            <a-select v-model:value="form.logLevel">
              <a-select-option value="DEBUG">DEBUG</a-select-option>
              <a-select-option value="INFO">INFO</a-select-option>
              <a-select-option value="WARN">WARN</a-select-option>
              <a-select-option value="ERROR">ERROR</a-select-option>
            </a-select>
          </a-form-item>
        </a-col>
        <a-col :span="8">
          <a-form-item :label="t('pages.naive.fields.extraArgs')">
            <a-input v-model:value="form.extraArgs" placeholder="--log-net-log=..." />
          </a-form-item>
        </a-col>
      </a-row>

      <a-row :gutter="12">
        <a-col :span="8">
          <a-form-item :label="t('pages.naive.fields.totalGB')">
            <a-input-number v-model:value="totalGB" :min="0" :step="1" style="width: 100%"
              :placeholder="t('pages.naive.unlimited')" />
          </a-form-item>
        </a-col>
        <a-col :span="8">
          <a-form-item :label="t('pages.naive.fields.expiryTime')">
            <a-date-picker v-model:value="expiryDate" show-time style="width: 100%"
              :placeholder="t('pages.naive.never')" />
          </a-form-item>
        </a-col>
        <a-col :span="8">
          <a-form-item :label="t('pages.naive.fields.trafficReset')">
            <a-select v-model:value="form.trafficReset">
              <a-select-option value="never">{{ t('pages.naive.reset.never') }}</a-select-option>
              <a-select-option value="day">{{ t('pages.naive.reset.day') }}</a-select-option>
              <a-select-option value="week">{{ t('pages.naive.reset.week') }}</a-select-option>
              <a-select-option value="month">{{ t('pages.naive.reset.month') }}</a-select-option>
            </a-select>
          </a-form-item>
        </a-col>
      </a-row>

      <a-divider style="margin: 4px 0 12px">{{ t('pages.naive.usersSection') }}</a-divider>
      <div class="naive-users">
        <div v-for="(u, idx) in form.users" :key="idx" class="naive-user-row">
          <a-input v-model:value="u.username" :placeholder="t('pages.naive.fields.authUser')" style="flex: 1" />
          <a-input-password v-model:value="u.password" :placeholder="t('pages.naive.fields.authPass')" style="flex: 1" />
          <a-tooltip :title="t('pages.naive.fields.enable')">
            <a-switch v-model:checked="u.enable" />
          </a-tooltip>
          <a-button danger type="text" @click="removeUser(idx)">
            <template #icon><DeleteOutlined /></template>
          </a-button>
        </div>
        <a-button type="dashed" block @click="addUser">
          <template #icon><PlusOutlined /></template>
          {{ t('pages.naive.addUser') }}
        </a-button>
      </div>
      </template>
    </a-form>
  </a-modal>
</template>

<style scoped>
.raw-editor :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  tab-size: 4;
}

.naive-users {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.naive-user-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>

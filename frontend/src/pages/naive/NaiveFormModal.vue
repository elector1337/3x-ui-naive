<script setup>
import { computed, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { message } from 'ant-design-vue';

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
    listen: '0.0.0.0',
    port: 443,
    domain: '',
    certFile: '',
    keyFile: '',
    authUser: '',
    authPass: '',
    padding: true,
    logLevel: 'WARNING',
    extraArgs: '',
  };
}

const form = ref(blank());
const saving = ref(false);

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
  if (!f.domain.trim()) return t('pages.naive.errDomain');
  if (!f.port || f.port < 1 || f.port > 65535) return t('pages.naive.errPort');
  if (!f.authUser.trim() || !f.authPass.trim()) return t('pages.naive.errAuth');
  if (!f.certFile.trim() || !f.keyFile.trim()) return t('pages.naive.errCert');
  return null;
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
        <a-col :span="16">
          <a-form-item :label="t('pages.naive.fields.remark')">
            <a-input v-model:value="form.remark" />
          </a-form-item>
        </a-col>
        <a-col :span="8">
          <a-form-item :label="t('pages.naive.fields.enable')">
            <a-switch v-model:checked="form.enable" />
          </a-form-item>
        </a-col>
      </a-row>

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
            <a-input v-model:value="form.listen" />
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

      <a-form-item :label="t('pages.naive.fields.certFile')" required>
        <a-input v-model:value="form.certFile" placeholder="/etc/letsencrypt/live/example.com/fullchain.pem" />
      </a-form-item>
      <a-form-item :label="t('pages.naive.fields.keyFile')" required>
        <a-input v-model:value="form.keyFile" placeholder="/etc/letsencrypt/live/example.com/privkey.pem" />
      </a-form-item>

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
              <a-select-option value="WARNING">WARNING</a-select-option>
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
    </a-form>
  </a-modal>
</template>

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Col,
  DatePicker,
  Divider,
  Form,
  Input,
  InputNumber,
  Modal,
  Row,
  Select,
  Space,
  Switch,
  Tooltip,
  message,
} from 'antd';
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import dayjs from 'dayjs';

import { HttpUtil } from '@/utils';
import type { Msg } from '@/utils';
import type { NaivePayload, NaiveServer, NaiveUser } from './useNaive';
import './NaivePage.css';

const GB = 1024 * 1024 * 1024;

interface NaiveFormModalProps {
  open: boolean;
  mode: 'add' | 'edit';
  server: NaiveServer | null;
  save: (payload: NaivePayload) => Promise<Msg<unknown>>;
  onOpenChange: (open: boolean) => void;
}

function blank(): NaivePayload {
  return {
    remark: '',
    enable: false,
    subId: '',
    listen: '',
    port: 443,
    domain: '',
    useAcme: false,
    acmeEmail: '',
    certFile: '',
    keyFile: '',
    authUser: '',
    authPass: '',
    ipLimit: 0,
    padding: true,
    enableH3: true,
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

function fromServer(server: NaiveServer): NaivePayload {
  const { id: _id, up: _up, down: _down, ...rest } = server;
  return { ...blank(), ...rest, users: (rest.users ?? []).map((u) => ({ ...u })) };
}

export default function NaiveFormModal({
  open,
  mode,
  server,
  save,
  onOpenChange,
}: NaiveFormModalProps) {
  const { t } = useTranslation();
  const [messageApi, messageContextHolder] = message.useMessage();
  // The page remounts this modal per open, so the initializer seeds the form.
  const [form, setForm] = useState<NaivePayload>(() => (server ? fromServer(server) : blank()));
  const [saving, setSaving] = useState(false);
  const [validating, setValidating] = useState(false);

  const set = <K extends keyof NaivePayload>(key: K, value: NaivePayload[K]) =>
    setForm((f) => ({ ...f, [key]: value }));

  const setUser = (idx: number, patch: Partial<NaiveUser>) =>
    setForm((f) => ({
      ...f,
      users: f.users.map((u, i) => (i === idx ? { ...u, ...patch } : u)),
    }));

  function validate(): string | null {
    const f = form;
    if (f.useRawConfig) {
      return f.rawConfig.trim() ? null : t('pages.naive.errRawEmpty');
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

  // Fills the raw editor with the Caddyfile the panel would generate from the form.
  async function prefillRaw() {
    const msg = await HttpUtil.post<string>(
      '/panel/api/naive/preview',
      { ...form, useRawConfig: false },
      { silentSuccess: true },
    );
    if (msg?.success && msg.obj) set('rawConfig', msg.obj);
  }

  async function runValidate() {
    if (!form.rawConfig.trim()) {
      messageApi.warning(t('pages.naive.errRawEmpty'));
      return;
    }
    setValidating(true);
    try {
      const msg = await HttpUtil.post(
        '/panel/api/naive/validate',
        { text: form.rawConfig },
        { silent: true },
      );
      if (msg?.success) messageApi.success(t('pages.naive.rawValid'));
      else messageApi.error(msg?.msg || t('pages.naive.rawInvalid'));
    } finally {
      setValidating(false);
    }
  }

  function onToggleRaw(v: boolean) {
    set('useRawConfig', v);
    if (v && !form.rawConfig.trim()) void prefillRaw();
  }

  async function onOk() {
    const err = validate();
    if (err) {
      messageApi.error(err);
      return;
    }
    setSaving(true);
    try {
      const msg = await save(form);
      if (msg?.success) onOpenChange(false);
    } finally {
      setSaving(false);
    }
  }

  const totalGB = form.total > 0 ? +(form.total / GB).toFixed(2) : 0;

  return (
    <Modal
      open={open}
      title={mode === 'edit' ? t('pages.naive.editTitle') : t('pages.naive.addTitle')}
      confirmLoading={saving}
      okText={t('save')}
      cancelText={t('cancel')}
      width={640}
      onOk={onOk}
      onCancel={() => onOpenChange(false)}
      destroyOnHidden
    >
      {messageContextHolder}
      <Form layout="vertical">
        <Row gutter={12}>
          <Col xs={24} sm={12}>
            <Form.Item label={t('pages.naive.fields.remark')}>
              <Input value={form.remark} onChange={(e) => set('remark', e.target.value)} />
            </Form.Item>
          </Col>
          <Col xs={12} sm={6}>
            <Form.Item label={t('pages.naive.fields.enable')}>
              <Switch checked={form.enable} onChange={(v) => set('enable', v)} />
            </Form.Item>
          </Col>
          <Col xs={12} sm={6}>
            <Form.Item label={t('pages.naive.fields.advanced')}>
              <Switch checked={form.useRawConfig} onChange={onToggleRaw} />
            </Form.Item>
          </Col>
        </Row>

        <Form.Item label={t('pages.naive.fields.subId')} help={t('pages.naive.subIdHint')}>
          <Input
            value={form.subId}
            placeholder="my-subscription"
            onChange={(e) => set('subId', e.target.value)}
          />
        </Form.Item>

        {form.useRawConfig ? (
          <>
            <Alert
              type="info"
              showIcon
              title={t('pages.naive.rawHint')}
              style={{ marginBottom: 12 }}
            />
            <Form.Item label={t('pages.naive.fields.rawConfig')} required>
              <Input.TextArea
                className="naive-raw-editor"
                value={form.rawConfig}
                autoSize={{ minRows: 12, maxRows: 24 }}
                spellCheck={false}
                onChange={(e) => set('rawConfig', e.target.value)}
              />
            </Form.Item>
            <Space style={{ marginBottom: 12 }}>
              <Button loading={validating} onClick={runValidate}>
                {t('pages.naive.validate')}
              </Button>
              <Button onClick={prefillRaw}>{t('pages.naive.regenerate')}</Button>
            </Space>
          </>
        ) : (
          <>
            <Row gutter={12}>
              <Col xs={24} sm={14}>
                <Form.Item label={t('pages.naive.fields.domain')} required>
                  <Input
                    value={form.domain}
                    placeholder="example.com"
                    onChange={(e) => set('domain', e.target.value)}
                  />
                </Form.Item>
              </Col>
              <Col xs={12} sm={6}>
                <Form.Item label={t('pages.naive.fields.port')} required>
                  <InputNumber
                    value={form.port}
                    min={1}
                    max={65535}
                    style={{ width: '100%' }}
                    onChange={(v) => set('port', v ?? 0)}
                  />
                </Form.Item>
              </Col>
              <Col xs={12} sm={4}>
                <Form.Item label={t('pages.naive.fields.listen')}>
                  <Input
                    value={form.listen}
                    placeholder="all"
                    onChange={(e) => set('listen', e.target.value)}
                  />
                </Form.Item>
              </Col>
            </Row>

            <Row gutter={12}>
              <Col xs={24} sm={9}>
                <Form.Item label={t('pages.naive.fields.authUser')} required>
                  <Input value={form.authUser} onChange={(e) => set('authUser', e.target.value)} />
                </Form.Item>
              </Col>
              <Col xs={24} sm={9}>
                <Form.Item label={t('pages.naive.fields.authPass')} required>
                  <Input.Password
                    value={form.authPass}
                    onChange={(e) => set('authPass', e.target.value)}
                  />
                </Form.Item>
              </Col>
              <Col xs={24} sm={6}>
                <Form.Item
                  label={t('pages.naive.fields.ipLimit')}
                  help={t('pages.naive.ipLimitHint')}
                >
                  <InputNumber
                    value={form.ipLimit}
                    min={0}
                    style={{ width: '100%' }}
                    placeholder={t('pages.naive.unlimited')}
                    onChange={(v) => set('ipLimit', v ?? 0)}
                  />
                </Form.Item>
              </Col>
            </Row>

            <Row gutter={12}>
              <Col xs={24} sm={10}>
                <Form.Item label={t('pages.naive.fields.useAcme')}>
                  <Switch checked={form.useAcme} onChange={(v) => set('useAcme', v)} />
                </Form.Item>
              </Col>
              {form.useAcme && (
                <Col xs={24} sm={14}>
                  <Form.Item label={t('pages.naive.fields.acmeEmail')} required>
                    <Input
                      value={form.acmeEmail}
                      placeholder="me@example.com"
                      onChange={(e) => set('acmeEmail', e.target.value)}
                    />
                  </Form.Item>
                </Col>
              )}
            </Row>

            {!form.useAcme && (
              <>
                <Form.Item label={t('pages.naive.fields.certFile')} required>
                  <Input
                    value={form.certFile}
                    placeholder="/etc/letsencrypt/live/example.com/fullchain.pem"
                    onChange={(e) => set('certFile', e.target.value)}
                  />
                </Form.Item>
                <Form.Item label={t('pages.naive.fields.keyFile')} required>
                  <Input
                    value={form.keyFile}
                    placeholder="/etc/letsencrypt/live/example.com/privkey.pem"
                    onChange={(e) => set('keyFile', e.target.value)}
                  />
                </Form.Item>
              </>
            )}

            <Row gutter={12}>
              <Col xs={12} sm={4}>
                <Form.Item label={t('pages.naive.fields.padding')}>
                  <Switch checked={form.padding} onChange={(v) => set('padding', v)} />
                </Form.Item>
              </Col>
              <Col xs={12} sm={4}>
                <Form.Item label={t('pages.naive.fields.enableH3')}>
                  <Switch checked={form.enableH3} onChange={(v) => set('enableH3', v)} />
                </Form.Item>
              </Col>
              <Col xs={24} sm={8}>
                <Form.Item label={t('pages.naive.fields.logLevel')}>
                  <Select
                    value={form.logLevel}
                    onChange={(v) => set('logLevel', v)}
                    options={['DEBUG', 'INFO', 'WARN', 'ERROR'].map((v) => ({
                      value: v,
                      label: v,
                    }))}
                  />
                </Form.Item>
              </Col>
              <Col xs={24} sm={8}>
                <Form.Item label={t('pages.naive.fields.extraArgs')}>
                  <Input
                    value={form.extraArgs}
                    placeholder="--log-net-log=..."
                    onChange={(e) => set('extraArgs', e.target.value)}
                  />
                </Form.Item>
              </Col>
            </Row>

            <Row gutter={12}>
              <Col xs={24} sm={8}>
                <Form.Item label={t('pages.naive.fields.totalGB')}>
                  <InputNumber
                    value={totalGB}
                    min={0}
                    step={1}
                    style={{ width: '100%' }}
                    placeholder={t('pages.naive.unlimited')}
                    onChange={(v) => set('total', v && v > 0 ? Math.round(v * GB) : 0)}
                  />
                </Form.Item>
              </Col>
              <Col xs={24} sm={8}>
                <Form.Item label={t('pages.naive.fields.expiryTime')}>
                  <DatePicker
                    showTime
                    style={{ width: '100%' }}
                    placeholder={t('pages.naive.never')}
                    value={form.expiryTime > 0 ? dayjs(form.expiryTime) : null}
                    onChange={(v) => set('expiryTime', v ? v.valueOf() : 0)}
                  />
                </Form.Item>
              </Col>
              <Col xs={24} sm={8}>
                <Form.Item label={t('pages.naive.fields.trafficReset')}>
                  <Select
                    value={form.trafficReset}
                    onChange={(v) => set('trafficReset', v)}
                    options={[
                      { value: 'never', label: t('pages.naive.reset.never') },
                      { value: 'day', label: t('pages.naive.reset.day') },
                      { value: 'week', label: t('pages.naive.reset.week') },
                      { value: 'month', label: t('pages.naive.reset.month') },
                    ]}
                  />
                </Form.Item>
              </Col>
            </Row>

            <Divider style={{ margin: '4px 0 12px' }}>{t('pages.naive.usersSection')}</Divider>
            <div className="naive-users">
              {form.users.map((u, idx) => (
                <div key={u.id ?? `new-${idx}`} className="naive-user-row">
                  <Input
                    value={u.username}
                    placeholder={t('pages.naive.fields.authUser')}
                    onChange={(e) => setUser(idx, { username: e.target.value })}
                  />
                  <Input.Password
                    value={u.password}
                    placeholder={t('pages.naive.fields.authPass')}
                    onChange={(e) => setUser(idx, { password: e.target.value })}
                  />
                  <Tooltip title={t('pages.naive.fields.enable')}>
                    <Switch checked={u.enable} onChange={(v) => setUser(idx, { enable: v })} />
                  </Tooltip>
                  <Button
                    danger
                    type="text"
                    icon={<DeleteOutlined />}
                    onClick={() =>
                      setForm((f) => ({ ...f, users: f.users.filter((_, i) => i !== idx) }))
                    }
                  />
                </div>
              ))}
              <Button
                type="dashed"
                block
                icon={<PlusOutlined />}
                onClick={() =>
                  setForm((f) => ({
                    ...f,
                    users: [...f.users, { username: '', password: '', enable: true }],
                  }))
                }
              >
                {t('pages.naive.addUser')}
              </Button>
            </div>
          </>
        )}
      </Form>
    </Modal>
  );
}

import { useCallback, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  Col,
  ConfigProvider,
  Empty,
  Layout,
  Modal,
  Result,
  Row,
  Space,
  Spin,
  Statistic,
  Table,
  Tag,
  Tooltip,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  CloudServerOutlined,
  CopyOutlined,
  DeleteOutlined,
  EditOutlined,
  FileTextOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  QrcodeOutlined,
  ReloadOutlined,
  UndoOutlined,
} from '@ant-design/icons';

import { useTheme } from '@/hooks/useTheme';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import AppSidebar from '@/layouts/AppSidebar';
import { ClipboardManager, SizeFormatter } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import { QrPanel } from '@/pages/inbounds/qr';
import NaiveFormModal from './NaiveFormModal';
import { useNaive } from './useNaive';
import type { NaivePayload, NaiveServer, NaiveStatus } from './useNaive';
import './NaivePage.css';

function urlFor(s: NaiveServer, user: string, pass: string) {
  return `naive+https://${encodeURIComponent(user)}:${encodeURIComponent(pass)}@${s.domain}:${s.port}`;
}

// The primary credential plus every enabled extra user, each its own URL / QR.
function credsFor(s: NaiveServer) {
  const list = [{ label: s.authUser, url: urlFor(s, s.authUser, s.authPass) }];
  for (const u of s.users || []) {
    if (u.enable && u.username)
      list.push({ label: u.username, url: urlFor(s, u.username, u.password) });
  }
  return list;
}

const nameOf = (s: NaiveServer | null) => (s ? s.remark || `naive-${s.id}` : '');
const isDepleted = (s: NaiveServer) => s.total > 0 && (s.up || 0) + (s.down || 0) >= s.total;
const isExpired = (s: NaiveServer) => s.expiryTime > 0 && Date.now() >= s.expiryTime;

export default function NaivePage() {
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const { isMobile } = useMediaQuery();
  const [modal, modalContextHolder] = Modal.useModal();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);

  const {
    servers,
    statuses,
    loading,
    fetched,
    fetchError,
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

  const [installing, setInstalling] = useState(false);
  const [installLog, setInstallLog] = useState('');
  const [formOpen, setFormOpen] = useState(false);
  const [formKey, setFormKey] = useState(0);
  const [formServer, setFormServer] = useState<NaiveServer | null>(null);
  const [logTarget, setLogTarget] = useState<NaiveServer | null>(null);
  const [logText, setLogText] = useState('');
  const [logLoading, setLogLoading] = useState(false);
  const [qrTarget, setQrTarget] = useState<NaiveServer | null>(null);

  const onInstallCaddy = useCallback(async () => {
    setInstalling(true);
    setInstallLog('');
    try {
      const res = await installCaddy((chunk) => setInstallLog((prev) => prev + chunk + '\n'));
      if (res.ok) messageApi.success(t('pages.naive.toasts.caddyInstalled'));
      else messageApi.error(res.err || t('pages.naive.toasts.caddyInstallFailed'));
    } catch (e) {
      messageApi.error(e instanceof Error ? e.message : String(e));
    } finally {
      setInstalling(false);
    }
  }, [installCaddy, messageApi, t]);

  const onAdd = useCallback(() => {
    setFormServer(null);
    setFormKey((k) => k + 1);
    setFormOpen(true);
  }, []);

  const onEdit = useCallback((s: NaiveServer) => {
    setFormServer({ ...s });
    setFormKey((k) => k + 1);
    setFormOpen(true);
  }, []);

  const onSave = useCallback(
    (payload: NaivePayload) => (formServer?.id ? update(formServer.id, payload) : create(payload)),
    [formServer, update, create],
  );

  const onDelete = useCallback(
    (s: NaiveServer) => {
      modal.confirm({
        title: t('pages.naive.deleteConfirmTitle', { name: s.remark || s.domain }),
        okText: t('delete'),
        okType: 'danger',
        cancelText: t('cancel'),
        onOk: async () => {
          const msg = await remove(s.id);
          if (msg?.success) messageApi.success(t('pages.naive.toasts.deleted'));
        },
      });
    },
    [modal, t, remove, messageApi],
  );

  const runAction = useCallback(
    async (fn: (id: number) => Promise<{ success: boolean }>, s: NaiveServer, toastKey: string) => {
      const msg = await fn(s.id);
      if (msg?.success) messageApi.success(t(toastKey));
    },
    [messageApi, t],
  );

  const loadLog = useCallback(
    async (s: NaiveServer) => {
      setLogLoading(true);
      try {
        const res = await fetchLog(s.id, 200);
        setLogText(res?.success ? res.obj || '' : res?.msg || t('pages.naive.logError'));
      } finally {
        setLogLoading(false);
      }
    },
    [fetchLog, t],
  );

  const onShowLog = useCallback(
    (s: NaiveServer) => {
      setLogTarget(s);
      setLogText('');
      void loadLog(s);
    },
    [loadLog],
  );

  const onCopy = useCallback(
    async (s: NaiveServer) => {
      if (await ClipboardManager.copyText(urlFor(s, s.authUser, s.authPass))) {
        messageApi.success(t('copied'));
      }
    },
    [messageApi, t],
  );

  const totals = useMemo(() => {
    const running = servers.filter((s) => statuses[s.id]?.running).length;
    return { total: servers.length, running, stopped: Math.max(0, servers.length - running) };
  }, [servers, statuses]);

  const statusTags = useCallback(
    (s: NaiveServer): ReactNode => {
      const st: NaiveStatus | undefined = statuses[s.id];
      let state: ReactNode;
      if (st?.running && st.listening && st.responding) {
        state = <Tag color="success">{t('pages.naive.running')}</Tag>;
      } else if (st?.running && st.listening) {
        state = <Tag color="warning">{t('pages.naive.unresponsive')}</Tag>;
      } else if (st?.running) {
        state = <Tag color="processing">{t('pages.naive.starting')}</Tag>;
      } else {
        state = <Tag>{t('pages.naive.stopped')}</Tag>;
      }
      return (
        <>
          {state}
          {isExpired(s) ? (
            <Tag color="error">{t('pages.naive.expired')}</Tag>
          ) : isDepleted(s) ? (
            <Tag color="error">{t('pages.naive.depleted')}</Tag>
          ) : null}
        </>
      );
    },
    [statuses, t],
  );

  const enableTag = useCallback(
    (s: NaiveServer) => (
      <Tag color={s.enable ? 'green' : 'default'}>
        {s.enable ? t('pages.naive.enabled') : t('pages.naive.disabled')}
      </Tag>
    ),
    [t],
  );

  const actions = useCallback(
    (s: NaiveServer, compact: boolean) => {
      const running = !!statuses[s.id]?.running;
      return (
        <Space size={compact ? 6 : 4} wrap className={compact ? 'naive-srv-actions' : undefined}>
          <Tooltip title={t('pages.naive.copyUrl')}>
            <Button size="small" icon={<CopyOutlined />} onClick={() => onCopy(s)} />
          </Tooltip>
          <Tooltip title={t('pages.naive.showQr')}>
            <Button size="small" icon={<QrcodeOutlined />} onClick={() => setQrTarget(s)} />
          </Tooltip>
          {running ? (
            <Button
              size="small"
              danger
              icon={<PauseCircleOutlined />}
              onClick={() => runAction(stop, s, 'pages.naive.toasts.stopped')}
            >
              {t('pages.naive.stop')}
            </Button>
          ) : (
            <Button
              size="small"
              type="primary"
              icon={<PlayCircleOutlined />}
              onClick={() => runAction(start, s, 'pages.naive.toasts.started')}
            >
              {t('pages.naive.start')}
            </Button>
          )}
          <Tooltip title={t('pages.naive.restart')}>
            <Button
              size="small"
              disabled={!running}
              icon={<ReloadOutlined />}
              onClick={() => runAction(restart, s, 'pages.naive.toasts.restarted')}
            />
          </Tooltip>
          <Tooltip title={t('pages.naive.viewLog')}>
            <Button size="small" icon={<FileTextOutlined />} onClick={() => onShowLog(s)} />
          </Tooltip>
          <Button size="small" icon={<EditOutlined />} onClick={() => onEdit(s)} />
          <Tooltip title={t('pages.naive.resetTraffic')}>
            <Button
              size="small"
              icon={<UndoOutlined />}
              onClick={() => runAction(resetTraffic, s, 'pages.naive.toasts.trafficReset')}
            />
          </Tooltip>
          <Button size="small" danger icon={<DeleteOutlined />} onClick={() => onDelete(s)} />
        </Space>
      );
    },
    [
      statuses,
      t,
      onCopy,
      runAction,
      stop,
      start,
      restart,
      resetTraffic,
      onShowLog,
      onEdit,
      onDelete,
    ],
  );

  const columns = useMemo<ColumnsType<NaiveServer>>(
    () => [
      { key: 'remark', title: t('pages.naive.fields.remark'), render: (_v, r) => nameOf(r) },
      {
        key: 'domain',
        title: t('pages.naive.fields.domain'),
        render: (_v, r) => (
          <code>
            {r.domain}:{r.port}
          </code>
        ),
      },
      {
        key: 'enable',
        title: t('pages.naive.fields.enable'),
        width: 110,
        render: (_v, r) => enableTag(r),
      },
      {
        key: 'status',
        title: t('pages.naive.statusCol'),
        width: 160,
        render: (_v, r) => statusTags(r),
      },
      {
        key: 'traffic',
        title: t('pages.naive.trafficCol'),
        width: 170,
        render: (_v, r) => (
          <Tooltip title={t('pages.naive.trafficHint')}>
            <span>
              ↑ {SizeFormatter.sizeFormat(r.up || 0)} / ↓ {SizeFormatter.sizeFormat(r.down || 0)}
            </span>
          </Tooltip>
        ),
      },
      {
        key: 'actions',
        title: '',
        width: 340,
        align: 'right',
        render: (_v, r) => actions(r, false),
      },
    ],
    [t, enableTag, statusTags, actions],
  );

  const pageClass = ['naive-page', isDark && 'is-dark', isUltra && 'is-ultra']
    .filter(Boolean)
    .join(' ');

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      {modalContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />

        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Spin spinning={!fetched} delay={200} description={t('loading')} size="large">
              {!fetched ? (
                <div className="loading-spacer" />
              ) : fetchError ? (
                <Result
                  status="error"
                  title={t('somethingWentWrong')}
                  subTitle={fetchError}
                  extra={
                    <Button type="primary" loading={loading} onClick={() => refresh()}>
                      {t('refresh')}
                    </Button>
                  }
                />
              ) : (
                <Row gutter={[isMobile ? 8 : 16, isMobile ? 8 : 12]}>
                  {!caddy.installed && (
                    <Col span={24}>
                      <Alert
                        type="warning"
                        showIcon
                        title={t('pages.naive.caddy.missingTitle')}
                        description={
                          caddy.goPresent
                            ? t('pages.naive.caddy.canInstall')
                            : t('pages.naive.caddy.noGo')
                        }
                        action={
                          caddy.goPresent ? (
                            <Button
                              size="small"
                              type="primary"
                              loading={installing}
                              onClick={onInstallCaddy}
                            >
                              {installing
                                ? t('pages.naive.caddy.installing')
                                : t('pages.naive.caddy.install')}
                            </Button>
                          ) : undefined
                        }
                      />
                    </Col>
                  )}
                  {(installing || installLog) && (
                    <Col span={24}>
                      <Card size="small">
                        <pre className="naive-install-log">{installLog || '...'}</pre>
                      </Card>
                    </Col>
                  )}
                  {caddy.installed && (
                    <Col span={24}>
                      <Alert
                        type="success"
                        showIcon
                        closable
                        title={t('pages.naive.caddy.installedTitle', { v: caddy.version || '?' })}
                        description={`${caddy.source}: ${caddy.path}`}
                      />
                    </Col>
                  )}

                  <Col span={24}>
                    <Card size="small" hoverable className="summary-card">
                      <Row gutter={[16, isMobile ? 16 : 12]}>
                        <Col span={8}>
                          <Statistic
                            title={t('pages.naive.totals.total')}
                            value={String(totals.total)}
                            prefix={<CloudServerOutlined />}
                          />
                        </Col>
                        <Col span={8}>
                          <Statistic
                            title={t('pages.naive.totals.running')}
                            value={String(totals.running)}
                            prefix={
                              <CheckCircleOutlined style={{ color: 'var(--ant-color-success)' }} />
                            }
                          />
                        </Col>
                        <Col span={8}>
                          <Statistic
                            title={t('pages.naive.totals.stopped')}
                            value={String(totals.stopped)}
                            prefix={
                              <CloseCircleOutlined style={{ color: 'var(--ant-color-error)' }} />
                            }
                          />
                        </Col>
                      </Row>
                    </Card>
                  </Col>

                  <Col span={24}>
                    <Card
                      size="small"
                      hoverable
                      className="list-card"
                      title={t('pages.naive.title')}
                      extra={
                        <Space>
                          <Button
                            icon={<ReloadOutlined />}
                            loading={loading}
                            onClick={() => refresh()}
                          >
                            {t('refresh')}
                          </Button>
                          <Button type="primary" icon={<PlusOutlined />} onClick={onAdd}>
                            {t('pages.naive.add')}
                          </Button>
                        </Space>
                      }
                    >
                      {isMobile ? (
                        <div className="naive-mobile-cards">
                          {servers.length === 0 && <Empty description={t('pages.naive.empty')} />}
                          {servers.map((srv) => (
                            <div key={srv.id} className="naive-srv-card">
                              <div className="naive-srv-head">
                                <div className="naive-srv-title">{nameOf(srv)}</div>
                                {statusTags(srv)}
                              </div>
                              <div className="naive-srv-row">
                                <span className="naive-srv-label">
                                  {t('pages.naive.fields.domain')}
                                </span>
                                <code>
                                  {srv.domain}:{srv.port}
                                </code>
                              </div>
                              <div className="naive-srv-row">
                                <span className="naive-srv-label">
                                  {t('pages.naive.fields.enable')}
                                </span>
                                {enableTag(srv)}
                              </div>
                              {actions(srv, true)}
                            </div>
                          ))}
                        </div>
                      ) : (
                        <Table<NaiveServer>
                          columns={columns}
                          dataSource={servers}
                          pagination={false}
                          rowKey="id"
                          size="middle"
                          scroll={{ x: 'max-content' }}
                          locale={{ emptyText: <Empty description={t('pages.naive.empty')} /> }}
                        />
                      )}
                    </Card>
                  </Col>
                </Row>
              )}
            </Spin>
          </Layout.Content>
        </Layout>

        <NaiveFormModal
          key={formKey}
          open={formOpen}
          mode={formServer ? 'edit' : 'add'}
          server={formServer}
          save={onSave}
          onOpenChange={setFormOpen}
        />

        <Modal
          open={!!logTarget}
          title={t('pages.naive.logTitle', { name: nameOf(logTarget) })}
          width={780}
          footer={null}
          onCancel={() => setLogTarget(null)}
          destroyOnHidden
        >
          <Space style={{ marginBottom: 8 }}>
            <Button
              size="small"
              icon={<ReloadOutlined />}
              loading={logLoading}
              onClick={() => logTarget && loadLog(logTarget)}
            >
              {t('refresh')}
            </Button>
            <span className="naive-log-hint">{t('pages.naive.logHint')}</span>
          </Space>
          <pre className="naive-log-view">{logText || t('pages.naive.logEmpty')}</pre>
        </Modal>

        <Modal
          open={!!qrTarget}
          title={t('pages.naive.qrTitle', { name: nameOf(qrTarget) })}
          width={420}
          footer={null}
          onCancel={() => setQrTarget(null)}
          destroyOnHidden
        >
          {qrTarget &&
            credsFor(qrTarget).map((cred) => (
              <QrPanel key={cred.label} value={cred.url} remark={cred.label} />
            ))}
        </Modal>
      </Layout>
    </ConfigProvider>
  );
}

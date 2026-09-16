import { useCallback, useMemo } from 'react';
import { useQueryClient, useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import type { Msg } from '@/utils';
import { postRaw } from '@/api/http-init';

export interface NaiveUser {
  id?: number;
  naiveId?: number;
  username: string;
  password: string;
  enable: boolean;
}

export interface NaiveServer {
  id: number;
  remark: string;
  enable: boolean;
  subId: string;
  listen: string;
  port: number;
  domain: string;
  useAcme: boolean;
  acmeEmail: string;
  certFile: string;
  keyFile: string;
  authUser: string;
  authPass: string;
  ipLimit: number;
  padding: boolean;
  enableH3: boolean;
  logLevel: string;
  extraArgs: string;
  useRawConfig: boolean;
  rawConfig: string;
  up: number;
  down: number;
  total: number;
  expiryTime: number;
  trafficReset: string;
  users: NaiveUser[];
}

export type NaivePayload = Omit<NaiveServer, 'id' | 'up' | 'down'>;

export interface NaiveStatus {
  id?: number;
  running: boolean;
  pid?: number;
  since?: number;
  logPath?: string;
  listening?: boolean;
  responding?: boolean;
}

export interface CaddyStatus {
  installed: boolean;
  path: string;
  source: string;
  version: string;
  goPresent: boolean;
}

export interface InstallResult {
  ok: boolean;
  log: string;
  err: string;
}

const API = '/panel/api/naive';
const silent = { silent: true };
// The panel toasts API errors; success toasts come from the page instead.
const quietOk = { silentSuccess: true };

const EMPTY_CADDY: CaddyStatus = {
  installed: false,
  path: '',
  source: '',
  version: '',
  goPresent: false,
};

async function fetchList(): Promise<NaiveServer[]> {
  const msg = await HttpUtil.get<NaiveServer[]>(`${API}/list`, undefined, silent);
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch naive servers');
  return Array.isArray(msg.obj) ? msg.obj : [];
}

async function fetchCaddy(): Promise<CaddyStatus> {
  const msg = await HttpUtil.get<CaddyStatus>(`${API}/caddy-status`, undefined, silent);
  return msg?.success && msg.obj ? msg.obj : EMPTY_CADDY;
}

async function fetchStatuses(ids: number[]): Promise<Record<number, NaiveStatus>> {
  const entries = await Promise.all(
    ids.map(async (id): Promise<[number, NaiveStatus]> => {
      try {
        const r = await HttpUtil.get<NaiveStatus>(`${API}/status/${id}`, undefined, silent);
        return [id, r?.success && r.obj ? r.obj : { running: false }];
      } catch {
        return [id, { running: false }];
      }
    }),
  );
  return Object.fromEntries(entries);
}

// CRUD + lifecycle for /panel/api/naive.
export function useNaive() {
  const queryClient = useQueryClient();
  const listQuery = useQuery({ queryKey: ['naive', 'list'], queryFn: fetchList });
  const caddyQuery = useQuery({ queryKey: ['naive', 'caddy'], queryFn: fetchCaddy });
  const servers = useMemo(() => listQuery.data ?? [], [listQuery.data]);
  const ids = useMemo(() => servers.map((s) => s.id), [servers]);
  const statusQuery = useQuery({
    queryKey: ['naive', 'status', ids],
    queryFn: () => fetchStatuses(ids),
    enabled: listQuery.isSuccess,
  });

  const refresh = useCallback(
    () => queryClient.invalidateQueries({ queryKey: ['naive'] }),
    [queryClient],
  );
  const refreshStatuses = useCallback(
    () => queryClient.invalidateQueries({ queryKey: ['naive', 'status'] }),
    [queryClient],
  );

  const withRefresh = useCallback(
    async <T>(p: Promise<Msg<T>>): Promise<Msg<T>> => {
      const msg = await p;
      if (msg?.success) await refresh();
      return msg;
    },
    [refresh],
  );

  const withStatuses = useCallback(
    async <T>(p: Promise<Msg<T>>): Promise<Msg<T>> => {
      const msg = await p;
      if (msg?.success) await refreshStatuses();
      return msg;
    },
    [refreshStatuses],
  );

  const create = useCallback(
    (payload: NaivePayload) => withRefresh(HttpUtil.post(`${API}/add`, payload, quietOk)),
    [withRefresh],
  );
  const update = useCallback(
    (id: number, payload: NaivePayload) =>
      withRefresh(HttpUtil.post(`${API}/update/${id}`, payload, quietOk)),
    [withRefresh],
  );
  const remove = useCallback(
    (id: number) => withRefresh(HttpUtil.post(`${API}/delete/${id}`, undefined, quietOk)),
    [withRefresh],
  );
  const resetTraffic = useCallback(
    (id: number) => withRefresh(HttpUtil.post(`${API}/reset-traffic/${id}`, undefined, quietOk)),
    [withRefresh],
  );
  const start = useCallback(
    (id: number) => withStatuses(HttpUtil.post(`${API}/start/${id}`, undefined, quietOk)),
    [withStatuses],
  );
  const stop = useCallback(
    (id: number) => withStatuses(HttpUtil.post(`${API}/stop/${id}`, undefined, quietOk)),
    [withStatuses],
  );
  const restart = useCallback(
    (id: number) => withStatuses(HttpUtil.post(`${API}/restart/${id}`, undefined, quietOk)),
    [withStatuses],
  );

  const fetchLog = useCallback(
    (id: number, tail = 200) => HttpUtil.get<string>(`${API}/log/${id}`, { tail }, silent),
    [],
  );

  // Streams xcaddy build output (SSE) and resolves once the build finishes.
  const installCaddy = useCallback(
    async (onChunk?: (line: string) => void): Promise<InstallResult> => {
      const res = await postRaw(`${API}/install-caddy`);
      if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
      if (!res.body) throw new Error('streaming not supported');
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = '';
      let log = '';
      let ok = true;
      let err = '';
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        let idx: number;
        while ((idx = buf.indexOf('\n\n')) !== -1) {
          const frame = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          const event = /^event:\s*(\S+)/m.exec(frame)?.[1] || 'message';
          const data = /^data:\s?(.*)$/m.exec(frame)?.[1] ?? '';
          if (event === 'error') {
            ok = false;
            err = data;
          } else if (event === 'done') {
            ok = true;
          } else {
            log += data + '\n';
            onChunk?.(data);
          }
        }
      }
      await queryClient.invalidateQueries({ queryKey: ['naive', 'caddy'] });
      return { ok, log, err };
    },
    [queryClient],
  );

  return {
    servers,
    statuses: statusQuery.data ?? {},
    loading: listQuery.isFetching,
    fetched: listQuery.isFetched,
    fetchError: listQuery.error ? listQuery.error.message : '',
    caddy: caddyQuery.data ?? EMPTY_CADDY,
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
  };
}

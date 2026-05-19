import { onMounted, ref, shallowRef } from 'vue';
import { HttpUtil } from '@/utils';

const BASE_PATH = window.X_UI_BASE_PATH || '';

// /panel/api/naive — CRUD + lifecycle
export function useNaive() {
  const servers = shallowRef([]);
  const statuses = ref({});
  const loading = ref(false);
  const fetched = ref(false);

  async function refresh() {
    loading.value = true;
    try {
      const msg = await HttpUtil.get('/panel/api/naive/list');
      if (msg?.success) {
        servers.value = Array.isArray(msg.obj) ? msg.obj : [];
      }
      fetched.value = true;
      await refreshCaddyStatus();
      await refreshStatuses();
    } finally {
      loading.value = false;
    }
  }

  const caddy = ref({ installed: false, path: '', source: '', version: '', goPresent: false });
  async function refreshCaddyStatus() {
    try {
      const msg = await HttpUtil.get('/panel/api/naive/caddy-status');
      if (msg?.success && msg.obj) caddy.value = msg.obj;
    } catch (_e) { /* keep stale */ }
  }

  async function refreshStatuses() {
    const next = {};
    for (const s of servers.value) {
      try {
        const r = await HttpUtil.get(`/panel/api/naive/status/${s.id}`);
        if (r?.success) next[s.id] = r.obj;
      } catch (_e) {
        next[s.id] = { running: false };
      }
    }
    statuses.value = next;
  }

  async function create(payload) {
    const msg = await HttpUtil.post('/panel/api/naive/add', payload);
    if (msg?.success) await refresh();
    return msg;
  }

  async function update(id, payload) {
    const msg = await HttpUtil.post(`/panel/api/naive/update/${id}`, payload);
    if (msg?.success) await refresh();
    return msg;
  }

  async function remove(id) {
    const msg = await HttpUtil.post(`/panel/api/naive/delete/${id}`);
    if (msg?.success) await refresh();
    return msg;
  }

  async function start(id) {
    const msg = await HttpUtil.post(`/panel/api/naive/start/${id}`);
    if (msg?.success) await refreshStatuses();
    return msg;
  }

  async function stop(id) {
    const msg = await HttpUtil.post(`/panel/api/naive/stop/${id}`);
    if (msg?.success) await refreshStatuses();
    return msg;
  }

  async function restart(id) {
    const msg = await HttpUtil.post(`/panel/api/naive/restart/${id}`);
    if (msg?.success) await refreshStatuses();
    return msg;
  }

  // installCaddy posts to the SSE endpoint and resolves after the build finishes.
  // Returns { ok, log } — the full xcaddy output for display.
  async function previewCaddyfile(payload) {
    return HttpUtil.post('/panel/api/naive/preview', payload);
  }

  async function validateCaddyfile(text) {
    return HttpUtil.post('/panel/api/naive/validate', { text });
  }

  async function installCaddy(onChunk) {
    const res = await fetch(BASE_PATH + 'panel/api/naive/install-caddy', { method: 'POST' });
    if (!res.body) throw new Error('streaming not supported');
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buf = '';
    let log = '';
    let ok = true;
    let err = '';
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      let idx;
      while ((idx = buf.indexOf('\n\n')) !== -1) {
        const frame = buf.slice(0, idx); buf = buf.slice(idx + 2);
        const event = /^event:\s*(\S+)/m.exec(frame)?.[1] || 'message';
        const data = (/^data:\s?(.*)$/m.exec(frame)?.[1]) ?? '';
        if (event === 'error') { ok = false; err = data; }
        else if (event === 'done') { ok = true; }
        else { log += data + '\n'; if (onChunk) onChunk(data); }
      }
    }
    await refreshCaddyStatus();
    return { ok, log, err };
  }

  onMounted(refresh);

  return {
    servers,
    statuses,
    loading,
    fetched,
    caddy,
    refresh,
    refreshStatuses,
    refreshCaddyStatus,
    installCaddy,
    previewCaddyfile,
    validateCaddyfile,
    create,
    update,
    remove,
    start,
    stop,
    restart,
  };
}

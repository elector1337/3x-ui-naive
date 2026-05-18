import { onMounted, ref, shallowRef } from 'vue';
import { HttpUtil } from '@/utils';

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
      await refreshStatuses();
    } finally {
      loading.value = false;
    }
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

  onMounted(refresh);

  return {
    servers,
    statuses,
    loading,
    fetched,
    refresh,
    refreshStatuses,
    create,
    update,
    remove,
    start,
    stop,
    restart,
  };
}

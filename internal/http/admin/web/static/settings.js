async function loadConfig() {
  try {
    const res = await api.get('/admin/api/config');
    const cfg = res.config || {};
    document.getElementById('cfg-proxy').value = cfg.outbound_proxy_url || '';
    document.getElementById('cfg-upstream').textContent  = cfg.upstream_base_url || '—';
    document.getElementById('cfg-authmode').textContent  = cfg.upstream_auth_mode || '—';
    document.getElementById('cfg-threshold').textContent = cfg.failover_threshold ?? '—';
    document.getElementById('cfg-cooldown').textContent  = cfg.cooldown_seconds ?? '—';
  } catch (err) {
    toastError(err.message);
  }
}

document.addEventListener('DOMContentLoaded', () => {
  loadConfig();

  document.getElementById('config-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const url = document.getElementById('cfg-proxy').value.trim();
    try {
      await api.put('/admin/api/config', { outbound_proxy_url: url });
      toastSuccess('已保存');
      await loadConfig();
    } catch (err) { toastError(err.message); }
  });

  document.getElementById('reset-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target);
    const key = (fd.get('new_api_key') || '').trim();
    if (!key) return;
    if (!confirm('确认重置下游 API Key？当前会话会失效。')) return;
    try {
      await api.post('/admin/api/downstream/reset', { new_api_key: key });
      toastInfo('已重置，即将跳转登录页…');
      setTimeout(() => { location.href = '/admin'; }, 800);
    } catch (err) { toastError(err.message); }
  });
});

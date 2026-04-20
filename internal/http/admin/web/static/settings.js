function syncRedirectEmptyState() {
  const empty = document.getElementById('model-redirect-empty');
  const rows = document.querySelectorAll('[data-model-redirect-row]');
  empty.classList.toggle('hidden', rows.length > 0);
}

function addRedirectRow(item = {}) {
  const template = document.getElementById('model-redirect-row-template');
  const list = document.getElementById('model-redirect-list');
  const fragment = template.content.cloneNode(true);
  const row = fragment.querySelector('[data-model-redirect-row]');
  row.querySelector('[data-role="downstream-model"]').value = item.downstream_model || '';
  row.querySelector('[data-role="upstream-model"]').value = item.upstream_model || '';
  list.appendChild(fragment);
  syncRedirectEmptyState();
}

function renderRedirects(items) {
  const list = document.getElementById('model-redirect-list');
  list.innerHTML = '';
  for (const item of items || []) addRedirectRow(item);
  syncRedirectEmptyState();
}

function collectRedirects() {
  const rows = Array.from(document.querySelectorAll('[data-model-redirect-row]'));
  const redirects = [];
  for (const row of rows) {
    const downstreamModel = row.querySelector('[data-role="downstream-model"]').value.trim();
    const upstreamModel = row.querySelector('[data-role="upstream-model"]').value.trim();
    if (!downstreamModel && !upstreamModel) continue;
    if (!downstreamModel || !upstreamModel) {
      throw new Error('每条模型重定向都需要同时填写下游模型名和上游模型名');
    }
    redirects.push({ downstream_model: downstreamModel, upstream_model: upstreamModel });
  }
  return redirects;
}

async function loadConfig() {
  try {
    const res = await api.get('/admin/api/config');
    const cfg = res.config || {};
    document.getElementById('cfg-proxy').value = cfg.outbound_proxy_url || '';
    document.getElementById('cfg-upstream').textContent  = cfg.upstream_base_url || '—';
    document.getElementById('cfg-authmode').textContent  = cfg.upstream_auth_mode || '—';
    document.getElementById('cfg-threshold').textContent = cfg.failover_threshold ?? '—';
    document.getElementById('cfg-cooldown').textContent  = cfg.cooldown_seconds ?? '—';
    renderRedirects(cfg.model_redirects || []);
  } catch (err) {
    toastError(err.message);
  }
}

document.addEventListener('DOMContentLoaded', () => {
  loadConfig();

  document.getElementById('add-redirect-btn').addEventListener('click', () => {
    addRedirectRow();
  });

  document.getElementById('model-redirect-list').addEventListener('click', (e) => {
    const button = e.target.closest('[data-action="remove-redirect"]');
    if (!button) return;
    button.closest('[data-model-redirect-row]').remove();
    syncRedirectEmptyState();
  });

  document.getElementById('config-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const url = document.getElementById('cfg-proxy').value.trim();
    try {
      await api.put('/admin/api/config', {
        outbound_proxy_url: url,
        model_redirects: collectRedirects(),
      });
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

let items = [];

async function loadList() {
  try {
    const res = await api.get('/admin/api/upstreams');
    items = (res.items || []).slice().sort((a, b) => a.priority - b.priority);
    render();
  } catch (err) {
    toastError(err.message);
  }
}

function statusBadge(it) {
  if (!it.is_enabled) return '<span class="badge badge-red">已停用</span>';
  if (it.cooldown_until && new Date(it.cooldown_until) > new Date()) {
    return '<span class="badge badge-amber">冷却中</span>';
  }
  return '<span class="badge badge-green">可用</span>';
}

function balanceCell(it) {
  const a = it.last_balance_total_available, u = it.last_balance_total_used, g = it.last_balance_total_granted;
  if (a == null && u == null && g == null) return '<span class="text-slate-400">—</span>';
  const t = it.last_balance_checked_at ? ` <span class="text-slate-400 text-xs">(${escapeHTML(fmtTime(it.last_balance_checked_at))})</span>` : '';
  return `<span class="font-mono">${fmtFloat(a)} / ${fmtFloat(u)} / ${fmtFloat(g)}</span>${t}`;
}

function render() {
  const tbody = document.getElementById('upstream-tbody');
  if (items.length === 0) {
    tbody.innerHTML = '<tr><td colspan="10" class="text-center text-slate-400 py-6">暂无上游 Key，点击右上角「新增 Key」添加。</td></tr>';
    return;
  }
  tbody.innerHTML = items.map((it, idx) => `
    <tr data-id="${it.id}">
      <td class="font-mono text-slate-400">${idx + 1}</td>
      <td class="font-medium">${escapeHTML(it.alias)}</td>
      <td class="font-mono text-slate-500">${escapeHTML(it.key_hint)}</td>
      <td>${statusBadge(it)}</td>
      <td class="font-mono">${it.consecutive_failures}</td>
      <td>${it.cooldown_until ? escapeHTML(fmtTime(it.cooldown_until)) : '<span class="text-slate-400">—</span>'}</td>
      <td>${balanceCell(it)}</td>
      <td class="max-w-[200px] truncate" title="${escapeHTML(it.last_error_summary || '')}">${escapeHTML(it.last_error_summary || '—')}</td>
      <td class="text-slate-500">${escapeHTML(fmtTime(it.updated_at))}</td>
      <td class="text-right">
        <button class="btn btn-ghost" data-action="up"      ${idx === 0 ? 'disabled' : ''}>↑</button>
        <button class="btn btn-ghost" data-action="down"    ${idx === items.length - 1 ? 'disabled' : ''}>↓</button>
        <button class="btn"           data-action="refresh">刷余额</button>
        <button class="btn"           data-action="toggle">${it.is_enabled ? '停用' : '启用'}</button>
        <button class="btn"           data-action="edit">编辑</button>
        <button class="btn btn-danger" data-action="delete">删除</button>
      </td>
    </tr>
  `).join('');
}

document.addEventListener('click', async (ev) => {
  const btn = ev.target.closest('button[data-action]');
  if (!btn) return;
  const tr = btn.closest('tr[data-id]');
  if (!tr) return;
  const id = Number(tr.dataset.id);
  const action = btn.dataset.action;
  try {
    if (action === 'refresh') {
      toastInfo('正在查询余额…');
      await api.post(`/admin/api/upstreams/${id}/refresh-balance`);
      toastSuccess('已刷新');
      await loadList();
    } else if (action === 'toggle') {
      const it = items.find((x) => x.id === id);
      const path = it.is_enabled ? 'disable' : 'enable';
      await api.post(`/admin/api/upstreams/${id}/${path}`);
      await loadList();
    } else if (action === 'delete') {
      if (!confirm('确认删除该上游 Key？')) return;
      await api.del(`/admin/api/upstreams/${id}`);
      toastSuccess('已删除');
      await loadList();
    } else if (action === 'edit') {
      openModal(items.find((x) => x.id === id));
    } else if (action === 'up' || action === 'down') {
      const idx = items.findIndex((x) => x.id === id);
      const swap = action === 'up' ? idx - 1 : idx + 1;
      if (swap < 0 || swap >= items.length) return;
      const next = items.slice();
      [next[idx], next[swap]] = [next[swap], next[idx]];
      await api.post('/admin/api/upstreams/reorder', { ids: next.map((x) => x.id) });
      await loadList();
    }
  } catch (err) {
    toastError(err.message);
  }
});

function openModal(existing) {
  const isEdit = !!existing;
  const root = document.getElementById('modal-root');
  root.innerHTML = `
    <div class="modal-backdrop">
      <div class="modal">
        <h3 class="text-lg font-semibold text-slate-900 mb-4">${isEdit ? '编辑上游 Key' : '新增上游 Key'}</h3>
        <form id="modal-form" class="space-y-3">
          <div>
            <label class="label">别名</label>
            <input class="field" name="alias" required value="${isEdit ? escapeHTML(existing.alias) : ''}" />
          </div>
          <div>
            <label class="label">API Key${isEdit ? '（留空保持不变）' : ''}</label>
            <input class="field" name="api_key" type="password" ${isEdit ? '' : 'required'} placeholder="${isEdit ? escapeHTML(existing.key_hint) : ''}" />
          </div>
          <div class="flex items-center gap-2">
            <input type="checkbox" id="modal-enabled" ${!isEdit || existing.is_enabled ? 'checked' : ''} />
            <label for="modal-enabled" class="text-sm text-slate-600">启用</label>
          </div>
          <div class="flex justify-end gap-2 pt-2">
            <button type="button" class="btn" id="modal-cancel">取消</button>
            <button type="submit" class="btn btn-primary">${isEdit ? '保存' : '创建'}</button>
          </div>
        </form>
      </div>
    </div>
  `;
  const close = () => { root.innerHTML = ''; };
  root.querySelector('#modal-cancel').addEventListener('click', close);
  root.querySelector('#modal-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target);
    const body = {
      alias: fd.get('alias'),
      is_enabled: root.querySelector('#modal-enabled').checked,
    };
    const key = (fd.get('api_key') || '').trim();
    if (isEdit) {
      if (key) body.api_key = key;
      try { await api.put(`/admin/api/upstreams/${existing.id}`, body); toastSuccess('已更新'); close(); await loadList(); }
      catch (err) { toastError(err.message); }
    } else {
      body.api_key = key;
      try { await api.post('/admin/api/upstreams', body); toastSuccess('已创建'); close(); await loadList(); }
      catch (err) { toastError(err.message); }
    }
  });
}

document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('reload-btn').addEventListener('click', loadList);
  document.getElementById('create-btn').addEventListener('click', () => openModal(null));
  loadList();
});

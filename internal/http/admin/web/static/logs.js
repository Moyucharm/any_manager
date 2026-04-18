let state = { route: '', result: '', limit: 50, offset: 0 };

async function loadLogs() {
  try {
    const p = new URLSearchParams();
    if (state.route)  p.set('route', state.route);
    if (state.result) p.set('result', state.result);
    p.set('limit', state.limit);
    p.set('offset', state.offset);
    const res = await api.get('/admin/api/logs?' + p.toString());
    render(res.items || []);
  } catch (err) {
    toastError(err.message);
  }
}

function resultCell(log) {
  if (log.success) return '<span class="badge badge-green">成功</span>';
  return '<span class="badge badge-red">失败</span>';
}

function statusCell(code) {
  if (!code) return '<span class="text-slate-400">—</span>';
  let cls = 'badge-slate';
  if (code >= 200 && code < 300) cls = 'badge-green';
  else if (code >= 400 && code < 500) cls = 'badge-amber';
  else if (code >= 500) cls = 'badge-red';
  return `<span class="badge ${cls} font-mono">${code}</span>`;
}

function render(logs) {
  const tbody = document.getElementById('log-tbody');
  if (logs.length === 0) {
    tbody.innerHTML = '<tr><td colspan="10" class="text-center text-slate-400 py-6">暂无日志。</td></tr>';
  } else {
    tbody.innerHTML = logs.map((log) => `
      <tr>
        <td class="font-mono text-xs">${escapeHTML(fmtTime(log.request_ts))}</td>
        <td>${escapeHTML(log.route || '—')}</td>
        <td class="font-mono text-xs">${escapeHTML(log.method || '—')}</td>
        <td>${escapeHTML(log.upstream_alias || '—')}</td>
        <td class="font-mono text-xs">${escapeHTML(log.model || '—')}</td>
        <td>${statusCell(log.status_code)}</td>
        <td>${resultCell(log)}</td>
        <td class="max-w-[260px] truncate" title="${escapeHTML(log.failure_reason || '')}">${escapeHTML(log.failure_reason || '—')}</td>
        <td class="font-mono">${fmtNumber(log.latency_ms)} ms</td>
        <td class="font-mono text-xs text-slate-500">${escapeHTML(log.request_id || '—')}</td>
      </tr>
    `).join('');
  }
  const start = state.offset + 1;
  const end = state.offset + logs.length;
  document.getElementById('pager-info').textContent =
    logs.length === 0 ? `offset=${state.offset}` : `第 ${start}–${end} 条`;
  document.getElementById('prev-btn').disabled = state.offset === 0;
  document.getElementById('next-btn').disabled = logs.length < state.limit;
}

document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('filter-form').addEventListener('submit', (e) => {
    e.preventDefault();
    state.route  = document.getElementById('f-route').value;
    state.result = document.getElementById('f-result').value;
    state.limit  = Number(document.getElementById('f-limit').value);
    state.offset = 0;
    loadLogs();
  });
  document.getElementById('prev-btn').addEventListener('click', () => {
    state.offset = Math.max(0, state.offset - state.limit);
    loadLogs();
  });
  document.getElementById('next-btn').addEventListener('click', () => {
    state.offset += state.limit;
    loadLogs();
  });
  loadLogs();
});

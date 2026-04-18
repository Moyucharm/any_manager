async function loadSummary() {
  try {
    const s = await api.get('/admin/api/summary');
    document.getElementById('stat-total').textContent        = fmtNumber(s.total_requests_24h);
    document.getElementById('stat-success-rate').textContent = s.total_requests_24h > 0 ? fmtPercent(s.success_rate_24h) : '—';
    document.getElementById('stat-availability').innerHTML   = s.availability
      ? '<span class="badge badge-green">可用</span>'
      : '<span class="badge badge-red">不可用</span>';
    document.getElementById('stat-active-alias').textContent = s.active_upstream_alias || '—';
    document.getElementById('stat-enabled').textContent      = fmtNumber(s.enabled_key_count);
    document.getElementById('stat-available').textContent    = fmtNumber(s.available_key_count);
  } catch (err) {
    toastError(err.message);
  }
}

document.addEventListener('DOMContentLoaded', () => {
  loadSummary();
  document.getElementById('refresh-btn').addEventListener('click', loadSummary);
  setInterval(loadSummary, 15000);
});

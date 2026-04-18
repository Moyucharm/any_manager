// 通用工具：fetch 封装、toast、格式化。
const api = {
  async request(method, url, body) {
    const opts = { method, headers: { 'Accept': 'application/json' }, credentials: 'same-origin' };
    if (body !== undefined) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    const res = await fetch(url, opts);
    const text = await res.text();
    let data = null;
    if (text) {
      try { data = JSON.parse(text); } catch (_) { data = { raw: text }; }
    }
    if (!res.ok) {
      const msg = (data && data.error && data.error.message) || `请求失败 (${res.status})`;
      const err = new Error(msg);
      err.status = res.status;
      err.data = data;
      throw err;
    }
    return data;
  },
  get(url)        { return this.request('GET', url); },
  post(url, b)    { return this.request('POST', url, b); },
  put(url, b)     { return this.request('PUT', url, b); },
  del(url)        { return this.request('DELETE', url); },
};

function toast(kind, message) {
  const root = document.getElementById('toast-root');
  if (!root) return;
  const el = document.createElement('div');
  el.className = `toast toast-${kind}`;
  el.textContent = message;
  root.appendChild(el);
  setTimeout(() => { el.style.opacity = '0'; el.style.transition = 'opacity 300ms'; }, 2500);
  setTimeout(() => el.remove(), 2900);
}
const toastSuccess = (m) => toast('success', m);
const toastError   = (m) => toast('error', m);
const toastInfo    = (m) => toast('info', m);

function fmtNumber(n) {
  if (n === null || n === undefined) return '—';
  return new Intl.NumberFormat('en-US').format(n);
}
function fmtFloat(n, digits = 2) {
  if (n === null || n === undefined) return '—';
  return Number(n).toFixed(digits);
}
function fmtPercent(n, digits = 2) {
  if (n === null || n === undefined) return '—';
  return `${Number(n).toFixed(digits)}%`;
}
function fmtTime(value) {
  if (!value) return '—';
  const d = new Date(value);
  if (isNaN(d.getTime())) return '—';
  const pad = (x) => String(x).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}
function escapeHTML(s) {
  if (s === null || s === undefined) return '';
  return String(s)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

// Handle 401: redirect to login.
window.addEventListener('unhandledrejection', (ev) => {
  if (ev.reason && ev.reason.status === 401) {
    location.href = '/admin';
  }
});

// Logout button (on layout pages).
document.addEventListener('DOMContentLoaded', () => {
  const btn = document.getElementById('logout-btn');
  if (btn) {
    btn.addEventListener('click', async () => {
      try { await api.post('/admin/api/logout', {}); } catch (_) {}
      location.href = '/admin';
    });
  }
});

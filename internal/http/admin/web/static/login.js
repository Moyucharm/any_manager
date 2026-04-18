document.addEventListener('DOMContentLoaded', async () => {
  const title    = document.getElementById('form-title');
  const subtitle = document.getElementById('form-subtitle');
  const loading  = document.getElementById('form-loading');
  const loginF   = document.getElementById('login-form');
  const bootF    = document.getElementById('bootstrap-form');

  try {
    const s = await api.get('/admin/api/bootstrap-status');
    loading.classList.add('hidden');
    if (s.initialized) {
      title.textContent = '管理员登录';
      subtitle.textContent = '输入下游 API Key 登录管理后台。';
      loginF.classList.remove('hidden');
    } else {
      title.textContent = '首次初始化';
      subtitle.textContent = '设置下游 API Key（同时作为管理员登录密码）。';
      bootF.classList.remove('hidden');
    }
  } catch (err) {
    loading.textContent = '加载失败：' + err.message;
    return;
  }

  loginF.addEventListener('submit', async (e) => {
    e.preventDefault();
    const pw = new FormData(e.target).get('password');
    try {
      await api.post('/admin/api/login', { password: pw });
      location.href = '/admin/dashboard';
    } catch (err) { toastError(err.message); }
  });

  bootF.addEventListener('submit', async (e) => {
    e.preventDefault();
    const key = new FormData(e.target).get('downstream_api_key');
    try {
      await api.post('/admin/api/bootstrap', { downstream_api_key: key });
      location.href = '/admin/dashboard';
    } catch (err) { toastError(err.message); }
  });
});

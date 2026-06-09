const API = '/api';
let sites = [];

async function api(method, path, body) {
  const opts = { method, headers: { 'Content-Type': 'application/json' } };
  const token = localStorage.getItem('auth_token');
  if (token) opts.headers['Authorization'] = 'Bearer ' + token;
  if (body) opts.body = JSON.stringify(body);
  const res = await fetch(API + path, opts);
  if (res.status === 401 && path !== '/login') {
    localStorage.removeItem('auth_token');
    showLogin();
    throw new Error('请先登录');
  }
  if (res.status === 204) return null;
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || '请求失败');
  return data;
}

function toast(msg, type = 'success') {
  const el = document.createElement('div');
  el.className = `toast toast-${type}`;
  el.textContent = msg;
  document.body.appendChild(el);
  setTimeout(() => el.remove(), 3000);
}

function timeAgo(iso) {
  if (!iso) return '-';
  const diff = (Date.now() - new Date(iso).getTime()) / 1000;
  if (diff < 0) return '刚刚';
  if (diff < 60) return Math.floor(diff) + '秒前';
  if (diff < 3600) return Math.floor(diff / 60) + '分钟前';
  if (diff < 86400) return Math.floor(diff / 3600) + '小时前';
  return Math.floor(diff / 86400) + '天前';
}

// Refresh all time-ago labels every 10 seconds without re-fetching data.
setInterval(() => {
  document.querySelectorAll('[data-time]').forEach(el => {
    el.textContent = timeAgo(el.dataset.time);
  });
}, 10000);

function statusDot(status) {
  return `<span class="status-dot status-${status || 'unknown'}"></span>`;
}

function fmtBalance(balance, unit) {
  if (unit === 'Token') {
    if (balance >= 1e9) return (balance / 1e9).toFixed(1) + 'B';
    if (balance >= 1e6) return (balance / 1e6).toFixed(1) + 'M';
    if (balance >= 1e3) return (balance / 1e3).toFixed(1) + 'K';
    return Math.floor(balance).toString();
  }
  const sym = unit === 'CNY' ? '¥' : '$';
  return sym + balance.toFixed(2);
}

function renderSites() {
  const list = document.getElementById('site-list');
  let totalBal = 0, alerts = 0;

  sites.forEach(s => {
    if ((s.balance_unit === 'USD' || s.balance_unit === 'CNY' || s.balance_unit === '') && s.status !== 'error') {
      totalBal += s.balance;
    }
    if (s.status === 'low') alerts++;
  });

  document.getElementById('stat-total').textContent = sites.length;
  document.getElementById('stat-balance').textContent = '$' + totalBal.toFixed(2);
  document.getElementById('stat-alerts').textContent = alerts;

  if (sites.length === 0) {
    list.innerHTML = '<div style="text-align:center;padding:40px;color:#94a3b8">暂无站点，点击"添加站点"开始。</div>';
    return;
  }

  list.innerHTML = sites.map(s => {
    const threshStr = (s.thresholds || []).join(', ');
    const lowBadge = s.status === 'low' ? '<span class="low-badge">⚠️</span>' : '';
    const balText = s.status === 'error' ? '<span style="color:#ef4444">查询失败</span>' : fmtBalance(s.balance, s.balance_unit);
    const errInfo = s.last_error ? `<br><span style="color:#ef4444;font-size:0.8rem">${s.last_error.substring(0, 60)}</span>` : '';
    const timeISO = s.last_check_at || '';

    return `<div class="site-card">
      <div class="site-info">
        <div class="site-name">${statusDot(s.status)} ${esc(s.name)} ${lowBadge}</div>
        <div class="site-meta">上次: <span data-time="${esc(timeISO)}">${timeAgo(timeISO)}</span> | 阈值: $${threshStr}${errInfo}</div>
      </div>
      <div class="site-balance">${balText}</div>
      <div class="site-actions">
        <button class="btn btn-secondary btn-sm" onclick="checkSite('${s.id}')">查询</button>
        <button class="btn btn-secondary btn-sm" onclick="showEditModal('${s.id}')">编辑</button>
        <button class="btn btn-danger btn-sm" onclick="deleteSite('${s.id}','${esc(s.name)}')">删除</button>
      </div>
    </div>`;
  }).join('');
}

function esc(str) {
  const d = document.createElement('div');
  d.textContent = str;
  return d.innerHTML;
}

async function loadSites() {
  try {
    sites = await api('GET', '/sites') || [];
    renderSites();
  } catch (e) {
    toast(e.message, 'error');
  }
}

async function loadSettings() {
  try {
    const s = await api('GET', '/settings');
    document.getElementById('set-interval').value = s.interval_minutes;
    document.getElementById('set-token').value = s.telegram_bot_token === '***configured***' ? '' : s.telegram_bot_token;
    document.getElementById('set-chatid').value = s.telegram_chat_id;
    if (s.telegram_bot_token === '***configured***') {
      document.getElementById('set-token').placeholder = '已配置（留空保持不变）';
    }
  } catch (_) {}
}

function showAddModal() {
  document.getElementById('modal-title').textContent = '添加站点';
  document.getElementById('edit-id').value = '';
  document.getElementById('f-name').value = '';
  document.getElementById('f-url').value = '';
  document.getElementById('f-username').value = '';
  document.getElementById('f-password').value = '';
  document.getElementById('f-password').placeholder = '留空则使用 API Key 查询';
  document.getElementById('f-userid').value = '';
  document.getElementById('f-key').value = '';
  document.getElementById('f-key').placeholder = 'sk-xxxxxxxx';
  document.getElementById('f-auth').value = 'bearer';
  document.getElementById('f-thresholds').value = '10';
  document.getElementById('modal').style.display = 'flex';
}

function showEditModal(id) {
  const s = sites.find(x => x.id === id);
  if (!s) return;
  document.getElementById('modal-title').textContent = '编辑站点';
  document.getElementById('edit-id').value = id;
  document.getElementById('f-name').value = s.name;
  document.getElementById('f-url').value = s.base_url;
  document.getElementById('f-username').value = s.username || '';
  document.getElementById('f-password').value = '';
  document.getElementById('f-password').placeholder = s.username ? '已配置（留空保持不变）' : '留空则使用 API Key 查询';
  document.getElementById('f-userid').value = s.user_id || '';
  document.getElementById('f-key').value = '';
  document.getElementById('f-key').placeholder = s.api_key_masked || '留空保持不变';
  document.getElementById('f-auth').value = s.auth_type;
  document.getElementById('f-thresholds').value = (s.thresholds || []).join(', ');
  document.getElementById('modal').style.display = 'flex';
}

function closeModal() {
  document.getElementById('modal').style.display = 'none';
  document.getElementById('f-key').placeholder = 'sk-xxxxxxxx';
  document.getElementById('f-password').placeholder = '留空则使用 API Key 查询';
}

function parseThresholds(str) {
  return str.split(/[,，]/).map(s => parseFloat(s.trim())).filter(n => n > 0).sort((a, b) => b - a);
}

async function submitSite() {
  const id = document.getElementById('edit-id').value;
  const name = document.getElementById('f-name').value.trim();
  const url = document.getElementById('f-url').value.trim();
  const username = document.getElementById('f-username').value.trim();
  const password = document.getElementById('f-password').value;
  const userid = document.getElementById('f-userid').value.trim();
  const key = document.getElementById('f-key').value.trim();
  const auth = document.getElementById('f-auth').value;
  const thresholds = parseThresholds(document.getElementById('f-thresholds').value);

  if (!name || !url) return toast('名称和地址不能为空', 'error');

  try {
    if (id) {
      const body = { name, base_url: url, auth_type: auth, thresholds };
      if (key) body.api_key = key;
      if (username) body.username = username;
      if (password) body.password = password;
      if (userid) body.user_id = parseInt(userid) || 0;
      await api('PUT', '/sites/' + id, body);
      toast('站点已更新');
    } else {
      if (!username && !key) return toast('用户名密码 或 API Key 至少填一项', 'error');
      const body = { name, base_url: url, auth_type: auth, thresholds };
      if (key) body.api_key = key;
      if (username) body.username = username;
      if (password) body.password = password;
      if (userid) body.user_id = parseInt(userid) || 0;
      await api('POST', '/sites', body);
      toast('站点已添加');
    }
    closeModal();
    await loadSites();
  } catch (e) {
    toast(e.message, 'error');
  }
}

async function deleteSite(id, name) {
  if (!confirm('确认删除"' + name + '"？')) return;
  try {
    await api('DELETE', '/sites/' + id);
    toast('已删除');
    await loadSites();
  } catch (e) {
    toast(e.message, 'error');
  }
}

async function checkSite(id) {
  try {
    await api('POST', '/sites/' + id + '/check');
    toast('查询完成');
    await loadSites();
  } catch (e) {
    toast(e.message, 'error');
  }
}

async function refreshAll() {
  const btn = document.getElementById('btn-refresh');
  btn.disabled = true;
  btn.textContent = '查询中...';
  try {
    await api('POST', '/check-all');
    toast('已开始刷新');
    setTimeout(async () => {
      await loadSites();
      btn.disabled = false;
      btn.textContent = '刷新全部';
    }, 3000);
  } catch (e) {
    toast(e.message, 'error');
    btn.disabled = false;
    btn.textContent = '刷新全部';
  }
}

function toggleSettings() {
  document.getElementById('settings-modal').style.display = 'flex';
  loadSettings();
}

function closeSettings() {
  document.getElementById('settings-modal').style.display = 'none';
}

async function saveSettings() {
  const interval = parseInt(document.getElementById('set-interval').value);
  const token = document.getElementById('set-token').value.trim();
  const chatid = document.getElementById('set-chatid').value.trim();

  const body = { interval_minutes: interval };
  if (token) body.telegram_bot_token = token;
  if (chatid) body.telegram_chat_id = chatid;

  try {
    await api('PUT', '/settings', body);
    toast('设置已保存');
    closeSettings();
  } catch (e) {
    toast(e.message, 'error');
  }
}

async function testTelegram() {
  try {
    await api('POST', '/telegram/test');
    toast('测试消息已发送');
  } catch (e) {
    toast(e.message, 'error');
  }
}

function showLogin() {
  document.getElementById('login-overlay').style.display = 'flex';
  document.getElementById('btn-logout').style.display = 'none';
}

function hideLogin() {
  document.getElementById('login-overlay').style.display = 'none';
  document.getElementById('btn-logout').style.display = '';
}

async function doLogin() {
  const username = document.getElementById('login-username').value;
  const pwd = document.getElementById('login-password').value;
  if (!pwd) return;
  try {
    const res = await fetch(API + '/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password: pwd }),
    });
    const data = await res.json();
    if (!res.ok) { toast(data.error || '登录失败', 'error'); return; }
    localStorage.setItem('auth_token', data.token);
    document.getElementById('login-username').value = '';
    document.getElementById('login-password').value = '';
    hideLogin();
    loadSites();
  } catch (e) {
    toast('登录失败', 'error');
  }
}

async function doLogout() {
  try { await api('POST', '/logout'); } catch (_) {}
  localStorage.removeItem('auth_token');
  showLogin();
}

loadSites();
setInterval(loadSites, 60000);

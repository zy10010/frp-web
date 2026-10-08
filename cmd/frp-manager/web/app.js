'use strict';

/* ---------- tiny DOM helpers ---------- */
function el(tag, className, text) {
  const n = document.createElement(tag);
  if (className) n.className = className;
  if (text !== undefined) n.textContent = text;
  return n;
}

function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

/* ---------- API client ---------- */
async function api(path, opts = {}) {
  const res = await fetch(path, {
    method: opts.method || 'GET',
    headers: opts.body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    credentials: 'same-origin',
  });
  if (res.status === 401 && !path.endsWith('/api/login')) {
    showLogin();
    throw new Error('unauthorized');
  }
  const ct = res.headers.get('content-type') || '';
  const data = ct.includes('json') ? await res.json() : await res.text();
  if (!res.ok) {
    const msg = (data && typeof data === 'object' && data.error) ? data.error : (res.status + ' ' + res.statusText);
    throw new Error(msg);
  }
  return data;
}

/* ---------- toast ---------- */
let toastTimer;
function toast(msg, ok = true) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.className = 'toast ' + (ok ? 'ok' : 'err');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.add('hidden'), 3000);
}

/* ---------- state ---------- */
const proxyTypes = ['tcp', 'udp', 'http', 'https', 'tcpmux', 'stcp', 'xtcp', 'sudp'];
const visitorTypes = ['stcp', 'xtcp', 'sudp'];
const schemaCache = {};
let currentPage = 'dashboard';

/* ---------- auth ---------- */
function showLogin() {
  document.getElementById('app').classList.add('hidden');
  document.getElementById('login').classList.remove('hidden');
}
function showApp() {
  document.getElementById('login').classList.add('hidden');
  document.getElementById('app').classList.remove('hidden');
}

async function init() {
  document.getElementById('login-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const err = document.getElementById('login-error');
    err.classList.add('hidden');
    try {
      await api('/api/login', {
        method: 'POST',
        body: { user: document.getElementById('login-user').value, password: document.getElementById('login-pass').value },
      });
      showApp();
      await navigate('dashboard');
    } catch (ex) {
      err.textContent = ex.message;
      err.classList.remove('hidden');
    }
  });

  document.getElementById('logout').addEventListener('click', async () => {
    try { await api('/api/logout', { method: 'POST' }); } catch (_) {}
    showLogin();
  });

  document.querySelectorAll('#nav button').forEach((b) => {
    b.addEventListener('click', () => navigate(b.dataset.page));
  });

  // Check whether we are already logged in.
  try {
    await api('/api/status');
    showApp();
    await navigate('dashboard');
  } catch (_) {
    showLogin();
  }
}

async function navigate(page) {
  currentPage = page;
  document.querySelectorAll('#nav button').forEach((b) => b.classList.toggle('active', b.dataset.page === page));
  document.querySelectorAll('.page').forEach((p) => p.classList.add('hidden'));
  document.getElementById('page-' + page).classList.remove('hidden');
  try {
    if (page === 'dashboard') await renderDashboard();
    else if (page === 'server') await renderServer();
    else if (page === 'client') await renderClient();
    else if (page === 'update') await renderUpdate();
    else if (page === 'settings') await renderSettings();
  } catch (ex) {
    toast(ex.message, false);
  }
}

/* ---------- generic schema form renderer ---------- */
function makeField(label, desc, input) {
  const d = el('div', 'field');
  d.appendChild(el('span', 'label', label));
  d.appendChild(input);
  if (desc) d.appendChild(el('div', 'desc', desc));
  return d;
}

function renderFields(container, fields, value, defs) {
  for (const f of fields) renderField(container, f, value, defs || {});
}

function renderField(container, f, value, defs) {
  const def = defs[f.key];
  const cur = value[f.key];

  if (f.kind === 'group') {
    const g = el('div', 'group');
    g.appendChild(el('div', 'group-title', f.label));
    if (f.description) g.appendChild(el('div', 'group-desc', f.description));
    let child = cur;
    if (child === undefined || child === null || typeof child !== 'object') child = {};
    value[f.key] = child;
    const childDefs = (def && typeof def === 'object' && !Array.isArray(def)) ? def : {};
    renderFields(g, f.children, child, childDefs);
    container.appendChild(g);
    return;
  }

  switch (f.kind) {
    case 'string': {
      const input = el('input');
      input.type = 'text';
      input.value = cur ?? '';
      if (def !== undefined && def !== null) input.placeholder = String(def);
      input.addEventListener('input', () => setOrDelete(value, f.key, input.value));
      container.appendChild(makeField(f.label, f.description, input));
      break;
    }
    case 'password': {
      const input = el('input');
      input.type = 'password';
      input.autocomplete = 'new-password';
      input.value = cur ?? '';
      if (def !== undefined && def !== null) input.placeholder = String(def);
      input.addEventListener('input', () => setOrDelete(value, f.key, input.value));
      container.appendChild(makeField(f.label, f.description, input));
      break;
    }
    case 'int': {
      const input = el('input');
      input.type = 'number';
      input.step = '1';
      input.value = (cur === undefined || cur === null) ? '' : cur;
      if (def !== undefined && def !== null) input.placeholder = String(def);
      input.addEventListener('input', () => {
        if (input.value === '') { delete value[f.key]; return; }
        const n = Number(input.value);
        value[f.key] = Number.isInteger(n) ? n : n;
      });
      container.appendChild(makeField(f.label, f.description, input));
      break;
    }
    case 'bool': {
      const input = el('input');
      input.type = 'checkbox';
      input.checked = !!cur;
      input.addEventListener('change', () => { value[f.key] = input.checked; });
      const d = el('div', 'field checkbox');
      d.appendChild(el('span', 'label', f.label));
      d.appendChild(input);
      if (f.description) d.appendChild(el('div', 'desc', f.description));
      container.appendChild(d);
      break;
    }
    case 'triBool': {
      const sel = el('select');
      const opts = [
        ['', 'Default (unset)'],
        ['true', 'True'],
        ['false', 'False'],
      ];
      for (const [v, l] of opts) {
        const o = el('option', null, l);
        o.value = v;
        sel.appendChild(o);
      }
      sel.value = cur === undefined || cur === null ? '' : String(cur);
      sel.addEventListener('change', () => {
        if (sel.value === '') delete value[f.key];
        else value[f.key] = sel.value === 'true';
      });
      container.appendChild(makeField(f.label, f.description, sel));
      break;
    }
    case 'select': {
      const sel = el('select');
      for (const o of f.options || []) {
        const opt = el('option', null, o.label);
        opt.value = o.value;
        sel.appendChild(opt);
      }
      sel.value = cur ?? '';
      sel.addEventListener('change', () => setOrDelete(value, f.key, sel.value));
      container.appendChild(makeField(f.label, f.description, sel));
      break;
    }
    case 'stringList': {
      const ta = el('textarea');
      ta.value = Array.isArray(cur) ? cur.join('\n') : '';
      if (Array.isArray(def)) ta.placeholder = def.join(', ');
      ta.addEventListener('input', () => {
        const arr = ta.value.split('\n').map((s) => s.trim()).filter(Boolean);
        if (arr.length === 0) delete value[f.key];
        else value[f.key] = arr;
      });
      container.appendChild(makeField(f.label, f.description + (f.description ? ' ' : '') + '(one per line)', ta));
      break;
    }
    case 'portsRange':
    case 'bandwidth': {
      const input = el('input');
      input.type = 'text';
      input.value = cur ?? '';
      if (def !== undefined && def !== null) input.placeholder = String(def);
      const hint = f.kind === 'portsRange' ? 'e.g. 1000-2000,3000' : 'e.g. 1MB or 512KB';
      input.addEventListener('input', () => setOrDelete(value, f.key, input.value));
      container.appendChild(makeField(f.label, (f.description ? f.description + ' ' : '') + hint, input));
      break;
    }
    case 'stringMap':
    case 'boolMap': {
      renderMapEditor(container, f, value);
      break;
    }
    case 'plugin': {
      renderPlugin(container, f, value);
      break;
    }
    case 'raw': {
      const ta = el('textarea');
      ta.style.fontFamily = 'monospace';
      ta.value = JSON.stringify(cur ?? null, null, 2);
      const d = makeField(f.label, f.description, ta);
      ta.addEventListener('input', () => {
        try {
          const parsed = JSON.parse(ta.value);
          if (parsed === null || parsed === undefined) delete value[f.key];
          else value[f.key] = parsed;
          d.classList.remove('invalid');
        } catch (_) {
          d.classList.add('invalid');
        }
      });
      container.appendChild(d);
      break;
    }
    default: {
      container.appendChild(el('div', 'muted', 'Unknown field: ' + f.key));
    }
  }
}

function setOrDelete(obj, key, val) {
  if (val === '' || val === undefined || val === null) delete obj[key];
  else obj[key] = val;
}

function renderMapEditor(container, f, value) {
  const wrap = el('div', 'field');
  wrap.appendChild(el('span', 'label', f.label));
  if (f.description) wrap.appendChild(el('div', 'desc', f.description));
  const rows = el('div');
  const obj = (typeof value[f.key] === 'object' && value[f.key] !== null && !Array.isArray(value[f.key]))
    ? value[f.key]
    : {};
  value[f.key] = obj;
  const isBool = f.kind === 'boolMap';

  function draw() {
    rows.innerHTML = '';
    for (const [k, v] of Object.entries(obj)) {
      const row = el('div', 'kv-row');
      const keyInput = el('input');
      keyInput.value = k;
      keyInput.placeholder = 'key';
      const valInput = el('input');
      if (isBool) {
        valInput.type = 'checkbox';
        valInput.checked = !!v;
        valInput.addEventListener('change', () => { obj[k] = valInput.checked; });
      } else {
        valInput.value = v ?? '';
        valInput.placeholder = 'value';
        valInput.addEventListener('input', () => { obj[k] = valInput.value; });
      }
      keyInput.addEventListener('input', () => {
        const nk = keyInput.value;
        if (nk && nk !== k) { obj[nk] = obj[k]; delete obj[k]; draw(); }
      });
      const del = el('button', null, '✕');
      del.addEventListener('click', () => { delete obj[k]; draw(); });
      row.appendChild(keyInput);
      row.appendChild(valInput);
      row.appendChild(del);
      rows.appendChild(row);
    }
    if (Object.keys(obj).length === 0) {
      rows.appendChild(el('div', 'muted', 'No entries'));
    }
  }
  draw();
  const add = el('button', 'secondary', '+ Add entry');
  add.type = 'button';
  add.addEventListener('click', () => { obj[''] = isBool ? false : ''; draw(); });
  wrap.appendChild(rows);
  wrap.appendChild(add);
  container.appendChild(wrap);
}

/* ---------- plugin editor (typed client/visitor plugin options) ---------- */
function renderPlugin(container, f, value) {
  const wrapper = el('div', 'group');
  wrapper.appendChild(el('div', 'group-title', f.label));
  if (f.description) wrapper.appendChild(el('div', 'group-desc', f.description));

  const typeSel = el('select');
  const noneOpt = el('option', null, 'None (disabled)');
  noneOpt.value = '';
  typeSel.appendChild(noneOpt);
  for (const t of (f.pluginTypes || [])) {
    const o = el('option', null, t);
    o.value = t;
    typeSel.appendChild(o);
  }

  const body = el('div');
  body.style.marginTop = '12px';
  wrapper.appendChild(typeSel);
  wrapper.appendChild(body);
  container.appendChild(wrapper);

  let cur = value[f.key];
  if (cur === undefined || cur === null || typeof cur !== 'object' || Array.isArray(cur)) {
    cur = undefined;
  }
  typeSel.value = (cur && cur.type) || '';

  async function draw() {
    body.innerHTML = '';
    const t = typeSel.value;
    if (!t) return;
    const prefix = (f.pluginKind === 'visitor') ? 'visitor-plugin' : 'plugin';
    const schemaKey = prefix + '/' + t;
    if (!schemaCache[schemaKey]) schemaCache[schemaKey] = await api('/api/schema/' + schemaKey);
    const sch = schemaCache[schemaKey];
    const fields = (sch.schema.children || []).filter((x) => x.key !== 'type');
    let obj = value[f.key];
    if (obj === undefined || obj === null || typeof obj !== 'object' || Array.isArray(obj)) obj = {};
    obj.type = t;
    value[f.key] = obj;
    renderFields(body, fields, obj, sch.defaults);
  }

  typeSel.addEventListener('change', () => {
    if (!typeSel.value) delete value[f.key];
    else value[f.key] = { type: typeSel.value };
    draw();
  });

  draw();
}

/* ---------- page: dashboard ---------- */
async function renderDashboard() {
  const page = document.getElementById('page-dashboard');
  page.innerHTML = '';
  const s = await api('/api/status');

  page.appendChild(el('h2', null, 'Dashboard'));
  const cards = el('div', 'cards');
  for (const name of ['frps', 'frpc']) {
    const st = s[name];
    const card = el('div', 'stat');
    card.appendChild(el('div', 'label', name + ' · v' + (name === 'frps' ? s.frpsVersion : s.frpcVersion || '?') ));
    card.appendChild(el('div', 'value', st.running ? 'Running' : 'Stopped'));
    const badge = el('span', 'badge ' + (st.running ? 'running' : 'stopped'), st.running ? 'PID ' + st.pid : 'stopped');
    card.appendChild(badge);
    const btnRow = el('div', 'row');
    btnRow.style.marginTop = '12px';
    const start = el('button', 'primary', 'Start');
    start.addEventListener('click', async () => { await api('/api/process/' + name + '/start', { method: 'POST' }); await renderDashboard(); });
    const stop = el('button', 'secondary', 'Stop');
    stop.addEventListener('click', async () => { await api('/api/process/' + name + '/stop', { method: 'POST' }); await renderDashboard(); });
    const restart = el('button', 'secondary', 'Restart');
    restart.addEventListener('click', async () => { await api('/api/process/' + name + '/restart', { method: 'POST' }); await renderDashboard(); });
    btnRow.appendChild(start);
    btnRow.appendChild(stop);
    btnRow.appendChild(restart);
    card.appendChild(btnRow);
    cards.appendChild(card);
  }
  page.appendChild(cards);

  const info = el('div', 'card');
  info.appendChild(el('h3', null, 'System'));
  const kv = el('div', 'form-grid');
  info.appendChild(kv);
  const rows = [
    ['frp-manager', s.managerVersion],
    ['frps', s.frpsVersion || 'not found'],
    ['frpc', s.frpcVersion || 'not found'],
    ['Platform', s.platform],
    ['Web port', s.webPort],
    ['Mirror', s.mirror || '(none)'],
  ];
  for (const [k, v] of rows) {
    const d = el('div', 'field');
    d.appendChild(el('span', 'label', k));
    d.appendChild(el('div', null, esc(v)));
    kv.appendChild(d);
  }
  page.appendChild(info);
}

/* ---------- page: server (frps) ---------- */
async function renderServer() {
  const page = document.getElementById('page-server');
  page.innerHTML = '';
  page.appendChild(el('h2', null, 'Server configuration (frps)'));
  const form = el('div', 'form');
  page.appendChild(form);

  let schema;
  if (!schemaCache.server) schemaCache.server = await api('/api/schema/server');
  schema = schemaCache.server;
  const cfg = await api('/api/config/server');

  const value = cfg.config || {};
  renderFields(form, schema.schema.children, value, schema.defaults);

  const actions = el('div', 'actions row');
  const save = el('button', 'primary', 'Save & Apply');
  const saveOnly = el('button', 'secondary', 'Save only');
  const exampleBtn = el('button', 'secondary', 'View example');
  save.addEventListener('click', async () => {
    await doSaveServer(value, true);
  });
  saveOnly.addEventListener('click', async () => {
    await doSaveServer(value, false);
  });
  exampleBtn.addEventListener('click', async () => { showExample('server'); });
  actions.appendChild(save);
  actions.appendChild(saveOnly);
  actions.appendChild(exampleBtn);
  page.appendChild(actions);
}

async function doSaveServer(value, restart) {
  if (document.querySelector('.field.invalid')) { toast('Fix invalid JSON fields first', false); return; }
  await api('/api/config/server', { method: 'PUT', body: { config: value } });
  if (restart) await api('/api/process/frps/restart', { method: 'POST' });
  toast('Server configuration saved' + (restart ? ' and frps restarted' : ''));
}

async function showExample(kind) {
  const text = await api('/api/example/' + kind);
  const w = window.open('', '_blank');
  w.document.write('<pre style="color:#ccc;background:#111;padding:16px;font-family:monospace">' + esc(text) + '</pre>');
}

/* ---------- page: client (frpc) ---------- */
async function renderClient() {
  const page = document.getElementById('page-client');
  page.innerHTML = '';
  page.appendChild(el('h2', null, 'Client configuration (frpc)'));
  const form = el('div', 'form');
  page.appendChild(form);

  if (!schemaCache.client) schemaCache.client = await api('/api/schema/client');
  const schema = schemaCache.client;
  const cfg = await api('/api/config/client');
  const value = cfg.config || {};
  if (!Array.isArray(value.proxies)) value.proxies = [];
  if (!Array.isArray(value.visitors)) value.visitors = [];

  // Common settings
  const common = el('div', 'group');
  common.appendChild(el('div', 'group-title', 'Common settings'));
  renderFields(common, schema.schema.children, value, schema.defaults);
  form.appendChild(common);

  // Proxies
  form.appendChild(await renderProxySection(value, 'proxies', proxyTypes, '/api/schema/proxy/'));

  // Visitors
  form.appendChild(await renderProxySection(value, 'visitors', visitorTypes, '/api/schema/visitor/'));

  const actions = el('div', 'actions row');
  const save = el('button', 'primary', 'Save & Apply');
  const saveOnly = el('button', 'secondary', 'Save only');
  const exampleBtn = el('button', 'secondary', 'View example');
  save.addEventListener('click', async () => { await doSaveClient(value, true); });
  saveOnly.addEventListener('click', async () => { await doSaveClient(value, false); });
  exampleBtn.addEventListener('click', async () => { showExample('client'); });
  actions.appendChild(save);
  actions.appendChild(saveOnly);
  actions.appendChild(exampleBtn);
  page.appendChild(actions);
}

async function doSaveClient(value, restart) {
  if (document.querySelector('.field.invalid')) { toast('Fix invalid JSON fields first', false); return; }
  await api('/api/config/client', { method: 'PUT', body: { config: value } });
  if (restart) await api('/api/process/frpc/restart', { method: 'POST' });
  toast('Client configuration saved' + (restart ? ' and frpc restarted' : ''));
}

async function renderProxySection(value, key, types, schemaPrefix) {
  const wrapper = el('div', 'group');
  wrapper.appendChild(el('div', 'group-title', key === 'proxies' ? 'Proxies' : 'Visitors'));
  const list = el('div', 'proxy-list');
  wrapper.appendChild(list);

  const arr = value[key];

  async function draw() {
    list.innerHTML = '';
    if (arr.length === 0) list.appendChild(el('div', 'muted', 'None'));
    for (let i = 0; i < arr.length; i++) {
      const item = arr[i];
      const card = el('div', 'proxy-card');
      const head = el('div', 'proxy-head');

      const typeSel = el('select');
      for (const t of types) {
        const o = el('option', null, t);
        o.value = t;
        typeSel.appendChild(o);
      }
      typeSel.value = item.type || types[0];
      typeSel.addEventListener('change', () => {
        const name = item.name;
        const enabled = item.enabled;
        arr[i] = { type: typeSel.value };
        if (name !== undefined) arr[i].name = name;
        if (enabled !== undefined) arr[i].enabled = enabled;
        draw();
      });

      const nameInput = el('input');
      nameInput.placeholder = 'name';
      nameInput.value = item.name || '';
      nameInput.addEventListener('input', () => { item.name = nameInput.value; });

      const del = el('button', 'danger', 'Delete');
      del.type = 'button';
      del.addEventListener('click', () => { arr.splice(i, 1); draw(); });

      head.appendChild(typeSel);
      head.appendChild(nameInput);
      head.appendChild(el('div', 'pname', ''));
      head.appendChild(del);
      card.appendChild(head);

      const body = el('div', 'proxy-body');
      const type = item.type || types[0];
      const schemaKey = key + '/' + type;
      if (!schemaCache[schemaKey]) schemaCache[schemaKey] = await api(schemaPrefix + type);
      const sch = schemaCache[schemaKey];
      const fields = (sch.schema.children || []).filter((f) => f.key !== 'type');
      // ensure type is set on the object
      item.type = type;
      renderFields(body, fields, item, sch.defaults);
      card.appendChild(body);
      list.appendChild(card);
    }
  }

  const addRow = el('div', 'row');
  addRow.style.marginTop = '12px';
  const addSel = el('select');
  for (const t of types) {
    const o = el('option', null, t);
    o.value = t;
    addSel.appendChild(o);
  }
  const addBtn = el('button', 'secondary', '+ Add ' + (key === 'proxies' ? 'proxy' : 'visitor'));
  addBtn.type = 'button';
  addBtn.addEventListener('click', () => {
    arr.push({ type: addSel.value, name: '' });
    draw();
  });
  addRow.appendChild(addSel);
  addRow.appendChild(addBtn);
  wrapper.appendChild(addRow);

  await draw();
  return wrapper;
}

/* ---------- page: update ---------- */
async function renderUpdate() {
  const page = document.getElementById('page-update');
  page.innerHTML = '';
  page.appendChild(el('h2', null, 'Updates'));

  const card = el('div', 'card');
  const status = el('div');
  card.appendChild(status);
  const actions = el('div', 'actions row');
  const checkBtn = el('button', 'secondary', 'Check for updates');
  const applyBtn = el('button', 'primary', 'Download & apply update');
  applyBtn.disabled = true;
  actions.appendChild(checkBtn);
  actions.appendChild(applyBtn);
  card.appendChild(actions);
  const log = el('div', 'logbox hidden');
  card.appendChild(log);
  page.appendChild(card);

  let latestRelease = null;

  async function doCheck() {
    status.innerHTML = 'Checking…';
    log.classList.add('hidden');
    applyBtn.disabled = true;
    try {
      const r = await api('/api/update/check');
      if (r.error) {
        status.innerHTML = '';
        const err = el('div', 'error', 'Check failed: ' + esc(r.error));
        status.appendChild(err);
        return;
      }
      const lines = [
        ['Current version', r.current || 'unknown'],
        ['Latest version', r.latest || 'unknown'],
        ['Platform', r.platform],
        ['Asset', r.assetName || 'n/a'],
      ];
      status.innerHTML = '';
      for (const [k, v] of lines) {
        const d = el('div', 'field');
        d.appendChild(el('span', 'label', k));
        d.appendChild(el('div', null, esc(v)));
        status.appendChild(d);
      }
      if (r.upToDate) {
        status.appendChild(el('div', 'badge running', 'Up to date'));
        applyBtn.disabled = true;
      } else {
        status.appendChild(el('div', 'badge stopped', 'Update available'));
        applyBtn.disabled = !r.assetName;
      }
    } catch (ex) {
      status.innerHTML = '';
      status.appendChild(el('div', 'error', esc(ex.message)));
    }
  }

  checkBtn.addEventListener('click', doCheck);

  applyBtn.addEventListener('click', async () => {
    applyBtn.disabled = true;
    log.classList.remove('hidden');
    log.textContent = 'Downloading and applying update…\n';
    try {
      const r = await api('/api/update/apply', { method: 'POST' });
      log.textContent += (r.message || 'Update applied') + '\n';
      toast('Update applied');
      await doCheck();
    } catch (ex) {
      log.textContent += 'Error: ' + ex.message + '\n';
      toast(ex.message, false);
    }
  });

  await doCheck();
}

/* ---------- page: settings ---------- */
async function renderSettings() {
  const page = document.getElementById('page-settings');
  page.innerHTML = '';
  page.appendChild(el('h2', null, 'Settings'));
  const s = await api('/api/settings');

  const form = el('div', 'form');
  page.appendChild(form);

  const webGroup = el('div', 'group');
  webGroup.appendChild(el('div', 'group-title', 'Web interface'));
  const webGrid = el('div', 'form-grid');
  webGroup.appendChild(webGrid);
  form.appendChild(webGroup);

  const fields = [
    { key: 'webAddr', label: 'Listen address', value: s.webAddr, type: 'text' },
    { key: 'webPort', label: 'Port', value: s.webPort, type: 'number' },
    { key: 'webUser', label: 'Username', value: s.webUser, type: 'text' },
    { key: 'webPassword', label: 'Password', value: s.webPassword, type: 'password' },
  ];
  const inputs = {};
  for (const f of fields) {
    const input = el('input');
    input.type = f.type;
    input.value = f.value ?? '';
    inputs[f.key] = input;
    webGrid.appendChild(makeField(f.label, null, input));
  }

  const pathsGroup = el('div', 'group');
  pathsGroup.appendChild(el('div', 'group-title', 'frp binary & config paths'));
  const pathsGrid = el('div', 'form-grid');
  pathsGroup.appendChild(pathsGrid);
  form.appendChild(pathsGroup);
  const pathFields = [
    { key: 'frpsPath', label: 'frps binary', value: s.frpsPath },
    { key: 'frpcPath', label: 'frpc binary', value: s.frpcPath },
    { key: 'frpsConfig', label: 'frps config', value: s.frpsConfig },
    { key: 'frpcConfig', label: 'frpc config', value: s.frpcConfig },
    { key: 'workDir', label: 'Working directory', value: s.workDir },
    { key: 'mirror', label: 'GitHub mirror (leave empty for none)', value: s.mirror },
  ];
  for (const f of pathFields) {
    const input = el('input');
    input.type = 'text';
    input.value = f.value ?? '';
    inputs[f.key] = input;
    pathsGrid.appendChild(makeField(f.label, null, input));
  }

  const autoGroup = el('div', 'group');
  autoGroup.appendChild(el('div', 'group-title', 'Auto start'));
  const cb1 = el('input');
  cb1.type = 'checkbox';
  cb1.checked = !!s.autostartFrps;
  const cb2 = el('input');
  cb2.type = 'checkbox';
  cb2.checked = !!s.autostartFrpc;
  autoGroup.appendChild(checkRow('Start frps automatically on boot', cb1));
  autoGroup.appendChild(checkRow('Start frpc automatically on boot', cb2));
  form.appendChild(autoGroup);

  const actions = el('div', 'actions row');
  const save = el('button', 'primary', 'Save settings');
  save.addEventListener('click', async () => {
    const body = {
      webAddr: inputs.webAddr.value,
      webPort: parseInt(inputs.webPort.value, 10),
      webUser: inputs.webUser.value,
      webPassword: inputs.webPassword.value,
      frpsPath: inputs.frpsPath.value,
      frpcPath: inputs.frpcPath.value,
      frpsConfig: inputs.frpsConfig.value,
      frpcConfig: inputs.frpcConfig.value,
      workDir: inputs.workDir.value,
      mirror: inputs.mirror.value,
      autostartFrps: cb1.checked,
      autostartFrpc: cb2.checked,
    };
    try {
      await api('/api/settings', { method: 'PUT', body });
      toast('Settings saved');
    } catch (ex) {
      toast(ex.message, false);
    }
  });
  actions.appendChild(save);
  page.appendChild(actions);
}

function checkRow(label, input) {
  const d = el('div', 'field checkbox');
  d.appendChild(el('span', 'label', label));
  d.appendChild(input);
  return d;
}

document.addEventListener('DOMContentLoaded', init);

// ---------------------------------------------------------------------------
// uni-app 安卓打包服务 · 前端
// ---------------------------------------------------------------------------
const $ = (id) => document.getElementById(id);

let uploadId = null;
let iconId = "";
let es = null;
let runtimeOn = false;
let busy = false;

let lastJobs = [];
let sortDesc = true;
let visible = 15;
let query = "";
let statusFilter = "";
let currentJob = null;

// --------------------------- 环境开关 ---------------------------
async function loadEnv() {
  try {
    const d = await (await fetch('/api/env')).json();
    $('envImage').textContent = d.image;
    $('envMem').textContent = d.dockerMem || '-';
    $('btnLogout').hidden = !d.auth;
  } catch (e) { /* 忽略 */ }
}

async function loadRuntime(silent) {
  try {
    renderRuntime(await (await fetch('/api/runtime')).json(), silent);
  } catch (e) {
    renderRuntime({ dockerOk: false, message: '无法连接服务' }, silent);
  }
}

function renderRuntime(d, silent) {
  runtimeOn = !!(d.dockerOk && d.running);
  $('envImage').textContent = d.image || '-';
  $('envMem').textContent = d.dockerMem || '-';

  const dot = $('envDot');
  dot.className = 'dot ' + (!d.dockerOk ? 'bad' : (d.running ? 'on' : 'off'));

  if (!d.dockerOk) {
    $('envText').textContent = 'Docker 未运行';
    $('envSub').textContent = d.message || '请先启动 Docker Desktop';
  } else if (d.running) {
    $('envText').textContent = '打包环境运行中';
    $('envSub').textContent = '停止状态下不占用内存，可随时切换';
  } else {
    $('envText').textContent = '打包环境已停止';
    $('envSub').textContent = d.message || '停止状态下不占内存';
  }

  $('btnStart').hidden = !(d.dockerOk && d.imageOk && !d.running);
  $('btnStop').hidden = !d.running;
  $('btnRestart').hidden = !d.running;

  $('packArea').hidden = !runtimeOn;
  $('cardEmpty').hidden = runtimeOn || !d.dockerOk;
}

async function runtimeAction(action) {
  if (busy) return;
  busy = true;
  ['btnStart', 'btnStop', 'btnRestart'].forEach((b) => { $(b).disabled = true; });
  $('envSub').textContent = { start: '启动中…', stop: '停止中…', restart: '重建中…' }[action];
  try {
    const r = await fetch('/api/runtime/' + action, { method: 'POST' });
    if (!r.ok) alert(await r.text());
    else renderRuntime(await r.json());
  } catch (e) {
    alert('操作失败：' + e.message);
  } finally {
    ['btnStart', 'btnStop', 'btnRestart'].forEach((b) => { $(b).disabled = false; });
    busy = false;
    loadRuntime(true);
  }
}

$('btnStart').onclick = () => runtimeAction('start');
$('btnStop').onclick = () => runtimeAction('stop');
$('btnRestart').onclick = () => runtimeAction('restart');

$('btnLogout').onclick = async () => {
  await fetch('/api/logout', { method: 'POST' });
  location.href = '/login';
};

// --------------------------- 上传 ---------------------------
const drop = $('drop');
const fileInput = $('file');

drop.onclick = () => fileInput.click();
drop.ondragover = (e) => { e.preventDefault(); drop.classList.add('over'); };
drop.ondragleave = () => drop.classList.remove('over');
drop.ondrop = (e) => {
  e.preventDefault();
  drop.classList.remove('over');
  if (e.dataTransfer.files.length) startUpload(e.dataTransfer.files[0]);
};
fileInput.onchange = () => { if (fileInput.files.length) startUpload(fileInput.files[0]); };

function startUpload(file) {
  if (!runtimeOn) { alert('请先启动打包环境'); return; }
  const bar = $('upBar'), meta = $('upMeta');
  const inner = bar.querySelector('i');
  bar.classList.add('on'); meta.classList.add('on');
  inner.style.width = '0%';
  $('fileName').textContent = file.name;
  meta.textContent = '上传中…';

  const t0 = performance.now();
  const xhr = new XMLHttpRequest();
  xhr.open('PUT', '/api/upload?name=' + encodeURIComponent(file.name), true);
  xhr.upload.onprogress = (e) => {
    if (!e.lengthComputable) return;
    inner.style.width = (e.loaded / e.total * 100).toFixed(1) + '%';
    const sec = (performance.now() - t0) / 1000;
    const speed = sec > 0 ? e.loaded / sec / 1024 / 1024 : 0;
    meta.textContent = `${fmtBytes(e.loaded)} / ${fmtBytes(e.total)} · ${speed.toFixed(1)} MB/s`;
  };
  xhr.onload = () => {
    if (xhr.status !== 200) { meta.textContent = '上传失败：' + xhr.responseText; return; }
    const d = JSON.parse(xhr.responseText);
    uploadId = d.uploadId;
    const sec = (performance.now() - t0) / 1000;
    meta.textContent = `上传完成 ${fmtBytes(d.size)} · 耗时 ${sec.toFixed(1)}s`;
    inner.style.width = '100%';

    const m = d.manifest || {};
    if (m.name) $('fName').value = m.name;
    if (m.appId) $('fAppId').value = m.appId;
    if (m.versionName) $('fVName').value = m.versionName;
    if (m.versionCode) $('fVCode').value = m.versionCode;
    $('fSrc').value = d.name + ` (${fmtBytes(d.size)})`;
    $('iconHint').textContent = d.iconHint
      ? `资源包内已含图标 ${d.iconHint}，默认使用它；选择文件可覆盖`
      : '资源包内未发现 static/app_icons，建议在这里上传一张图标';

    autoFillAppKey(m.appId);
    suggestVersion(m.versionName, m.versionCode);
    validate();
  };
  xhr.onerror = () => { meta.textContent = '上传失败：网络错误'; };
  xhr.send(file);
}

async function autoFillAppKey(appid) {
  if (!appid || $('fAppKey').value) return;
  try {
    const d = await (await fetch('/api/appkey?appid=' + encodeURIComponent(appid))).json();
    if (d.appKey && !$('fAppKey').value) { $('fAppKey').value = d.appKey; validate(); }
  } catch (e) { /* 忽略 */ }
}

// 资源包没写版本时，按同包名上一条记录推一个
function suggestVersion(vName, vCode) {
  if (vName && vCode) return;
  const last = lastByPackage($('fPkg').value.trim());
  if (!last) return;
  if (!vName) $('fVName').value = bumpName(last.versionName);
  if (!vCode) $('fVCode').value = String((parseInt(last.versionCode, 10) || 0) + 1);
}

function lastByPackage(pkg) {
  if (!pkg) return null;
  const hits = lastJobs.filter((j) => j.package === pkg && j.status === 'done');
  hits.sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt));
  return hits[0] || null;
}

function bumpName(v) {
  const parts = String(v || '1.0.0').split('.');
  const n = parseInt(parts[parts.length - 1], 10);
  parts[parts.length - 1] = String(isNaN(n) ? 1 : n + 1);
  return parts.join('.');
}

$('btnBump').onclick = () => {
  const last = lastByPackage($('fPkg').value.trim());
  if (!last) { $('submitNote').textContent = '没有找到同包名的历史记录'; return; }
  $('fVName').value = bumpName(last.versionName);
  $('fVCode').value = String((parseInt(last.versionCode, 10) || 0) + 1);
  validate();
};

// --------------------------- 表单校验 ---------------------------
const PKG_RE = /^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$/;
const VNAME_RE = /^\d+(\.\d+){0,3}$/;

document.querySelectorAll('input[name=ks]').forEach((el) => {
  el.onchange = () => {
    const v = document.querySelector('input[name=ks]:checked').value;
    $('boxSaved').hidden = v !== 'saved';
    $('boxNew').hidden = v !== 'new';
    $('ksHint').textContent = v === 'auto'
      ? '测试证书仅供调试，正式发版请使用自己的 jks'
      : (v === 'saved' ? '证书与口令保存在服务端 data/keystores，仅供内网使用' : '上传后会自动保存，下次可直接选择');
    validate();
  };
});

function validate() {
  const pkg = $('fPkg').value.trim();
  const vname = $('fVName').value.trim();
  const vcode = $('fVCode').value.trim();
  const mode = document.querySelector('input[name=ks]:checked').value;

  const ePkg = !pkg ? '' : (PKG_RE.test(pkg) ? '' : '格式应类似 com.company.app（至少两段，字母开头）');
  $('ePkg').textContent = ePkg;
  const eVName = !vname ? '' : (VNAME_RE.test(vname) ? '' : '只允许数字和点，如 2.0.8');
  $('eVName').textContent = eVName;
  const eVCode = !vcode ? '' : (/^\d+$/.test(vcode) ? '' : '必须是整数');
  $('eVCode').textContent = eVCode;

  let ok = !!uploadId && !!pkg && !ePkg && !eVName && !eVCode;
  let note = '';
  if (!uploadId) note = '请先上传资源包并填写包名';
  else if (!pkg) note = '包名 applicationId 必填';
  else if (ePkg) note = '包名格式不对';
  else if (!$('fAppKey').value.trim()) note = '建议填写 appkey，否则真机提示 appkey 错误';
  else if (mode === 'saved' && !$('fKs').value) { ok = false; note = '请选择一个证书'; }
  else if (mode === 'new' && !$('fKsFile').files.length) { ok = false; note = '请选择证书文件'; }

  $('btnSubmit').disabled = !ok;
  $('submitNote').textContent = note;
}

['fPkg', 'fVName', 'fVCode', 'fKs', 'fAppKey'].forEach((id) => {
  $(id).addEventListener('input', validate);
  $(id).addEventListener('change', validate);
});
$('fKsFile').addEventListener('change', validate);

// --------------------------- 证书列表 ---------------------------
async function loadKeystores() {
  const list = await (await fetch('/api/keystores')).json();
  const sel = $('fKs');
  const keep = sel.value;
  sel.innerHTML = '';
  if (!list.length) {
    sel.innerHTML = '<option value="">（暂无已保存的证书）</option>';
  } else {
    list.forEach((k) => {
      const o = document.createElement('option');
      o.value = k.id;
      o.textContent = `${k.name} · ${k.alias} · ${k.createdAt}`;
      sel.appendChild(o);
    });
    if ([...sel.options].some((o) => o.value === keep)) sel.value = keep;
  }
  validate();
}

// --------------------------- 提交 ---------------------------
$('btnSubmit').onclick = async () => {
  if (!runtimeOn) { alert('请先启动打包环境'); return; }
  if (!$('fAppKey').value.trim() &&
      !confirm('没有填写 appkey，打出的包真机会提示「appkey未配置或配置错误」。仍要继续吗？')) {
    return;
  }

  const mode = document.querySelector('input[name=ks]:checked').value;
  let keystore = 'auto';

  if (mode === 'new') {
    const f = $('fKsFile').files[0];
    const q = new URLSearchParams({
      name: $('fKsName').value || f.name,
      alias: $('fKsAlias').value,
      storePass: $('fKsStore').value,
      keyPass: $('fKsKey').value,
    });
    const r = await fetch('/api/keystore?' + q, { method: 'PUT', body: f });
    if (!r.ok) { alert('证书上传失败：' + await r.text()); return; }
    keystore = (await r.json()).id;
    document.querySelector('input[name=ks][value=saved]').checked = true;
    document.querySelector('input[name=ks][value=saved]').dispatchEvent(new Event('change'));
    await loadKeystores();
    $('fKs').value = keystore;
  } else if (mode === 'saved') {
    keystore = $('fKs').value;
  }

  const iconFile = $('fIcon').files[0];
  if (iconFile) {
    const r = await fetch('/api/icon', { method: 'PUT', body: iconFile });
    if (!r.ok) { alert('图标上传失败：' + await r.text()); return; }
    iconId = (await r.json()).id;
  } else {
    iconId = "";
  }

  const body = {
    uploadId,
    appId: $('fAppId').value.trim(),
    appName: $('fName').value.trim(),
    package: $('fPkg').value.trim(),
    versionName: $('fVName').value.trim(),
    versionCode: $('fVCode').value.trim(),
    appKey: $('fAppKey').value.trim(),
    keystore,
    icon: iconId,
  };

  const r = await fetch('/api/build', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!r.ok) { alert('提交失败：' + await r.text()); return; }
  watchJob((await r.json()).id);
};

// --------------------------- SSE 进度 ---------------------------
function watchJob(id) {
  if (es) es.close();
  currentJob = id;
  $('cardRun').hidden = false;
  $('pLog').innerHTML = '';
  $('pPct').textContent = '0%';
  $('pStage').textContent = '排队中';
  $('pBadge').innerHTML = '<span class="badge queued">排队中</span>';
  $('pBar').className = 'bar';
  $('pBar').querySelector('i').style.width = '0%';
  $('runTitle').textContent = `${$('fName').value || ''} ${$('fVName').value || ''}`.trim();
  $('btnDlLog').href = `/api/jobs/${id}/log?download=1`;
  $('btnDlLog').setAttribute('download', '');
  $('logNote').textContent = '';
  $('cardRun').scrollIntoView({ behavior: 'smooth', block: 'center' });

  es = new EventSource('/api/jobs/' + id + '/events');
  es.addEventListener('snapshot', (ev) => {
    const d = JSON.parse(ev.data);
    $('pLog').innerHTML = '';
    (d.logs || []).forEach(appendLog);
    applyState(d.status, d.stage, d.progress);
  });
  es.addEventListener('log', (ev) => appendLog(JSON.parse(ev.data).line));
  es.addEventListener('state', (ev) => {
    const d = JSON.parse(ev.data);
    applyState(d.status, d.stage, d.progress);
    if (d.status === 'done' || d.status === 'failed') {
      es.close(); es = null;
      loadJobs();
    }
  });
  es.onerror = () => { /* 浏览器自动重连 */ };
}

function applyState(status, stage, progress) {
  const p = progress || 0;
  $('pPct').textContent = p + '%';
  $('pStage').textContent = stage || '';
  $('pBar').querySelector('i').style.width = p + '%';
  $('pBar').className = 'bar' + (status === 'done' ? ' done' : status === 'failed' ? ' fail' : '');
  const map = { done: ['done', '完成'], failed: ['failed', '失败'], running: ['running', '构建中'], queued: ['queued', '排队中'] };
  const [cls, txt] = map[status] || ['queued', status];
  $('pBadge').innerHTML = `<span class="badge ${cls}">${txt}</span>`;
}

function appendLog(line) {
  const el = document.createElement('div');
  let cls = '';
  if (/✓|BUILD SUCCESSFUL/.test(line)) cls = 'l-ok';
  else if (/^!|Warning|warning/.test(line)) cls = 'l-warn';
  else if (/✗|FAILED|error|Error|错误/.test(line)) cls = 'l-err';
  else if (/^━━|^> Task /.test(line)) cls = 'l-step';
  if (cls) el.className = cls;
  el.textContent = line;
  const box = $('pLog');
  const atBottom = box.scrollTop + box.clientHeight >= box.scrollHeight - 30;
  box.appendChild(el);
  while (box.childNodes.length > 600) box.removeChild(box.firstChild);
  if (atBottom) box.scrollTop = box.scrollHeight;
}

$('btnCopyLog').onclick = async () => {
  if (!currentJob) return;
  const txt = [...$('pLog').childNodes].map((n) => n.textContent).join('\n');
  try {
    await navigator.clipboard.writeText(txt);
    $('logNote').textContent = `已复制 ${txt.length} 字符`;
  } catch (e) {
    $('logNote').textContent = '复制失败，请用「下载日志」';
  }
  setTimeout(() => { $('logNote').textContent = ''; }, 3000);
};

// --------------------------- 记录列表 ---------------------------
function jobTime(j) {
  const t = Date.parse(j.createdAt || '');
  return isNaN(t) ? 0 : t;
}

function sortJobs(list) {
  return list.slice().sort((a, b) => {
    const d = jobTime(b) - jobTime(a);
    if (d !== 0) return sortDesc ? d : -d;
    return sortDesc ? String(b.id).localeCompare(String(a.id)) : String(a.id).localeCompare(String(b.id));
  });
}

async function loadJobs() {
  lastJobs = await (await fetch('/api/jobs', { cache: 'no-store' })).json();
  renderJobs();
}

function filteredJobs() {
  const q = query.trim().toLowerCase();
  return sortJobs(lastJobs).filter((j) => {
    if (statusFilter && j.status !== statusFilter) return false;
    if (!q) return true;
    return [j.appName, j.package, j.versionName, j.versionCode, j.apk, j.source]
      .some((v) => String(v || '').toLowerCase().includes(q));
  });
}

function renderJobs() {
  const all = filteredJobs();
  const list = all.slice(0, visible);
  const body = $('jobsBody');
  body.innerHTML = '';

  $('jobsEmpty').hidden = all.length > 0;
  $('jobsEmpty').textContent = lastJobs.length ? '没有匹配的记录' : '暂无记录';
  $('jobsCount').textContent = lastJobs.length
    ? `${all.length}/${lastJobs.length} 条 · 时间${sortDesc ? '倒序' : '正序'}` : '';
  $('sortArrow').textContent = sortDesc ? '↓' : '↑';
  $('btnMore').hidden = all.length <= visible;

  list.forEach((j) => {
    const tr = document.createElement('tr');
    tr.className = 'row';
    tr.title = '点击回填这条配置';
    const badge = {
      done: ['done', '完成'], failed: ['failed', '失败'],
      running: ['running', '构建中'], queued: ['queued', '排队中'],
    }[j.status] || ['queued', j.status];

    let ops = '';
    if (j.status === 'done' && j.apk) {
      ops = `<button class="btn sm" data-fast="${j.id}" data-name="${j.apk}" data-size="${j.apkSize}">加速下载</button>`;
    } else if (j.status === 'failed') {
      ops = `<button class="btn sm" data-retry="${j.id}">重试</button>`;
    }
    if (j.status === 'done' || j.status === 'failed') {
      ops += `<button class="btn sm ghost" data-del="${j.id}">删除</button>`;
    }

    const err = j.status === 'failed' && j.error
      ? `<div class="err">${escapeHtml(friendlyError(j.error))}</div>` : '';

    // 包信息（下载前就能看到）
    let meta = [];
    if (j.minSdk) meta.push(`minSdk ${j.minSdk}`);
    if (j.targetSdk) meta.push(`target ${j.targetSdk}`);
    if (j.apkSize) meta.push(fmtBytes(j.apkSize));
    const testTag = j.status === 'done' && j.testSigned
      ? '<span class="tag warn" title="使用自动生成的测试证书签名，不可上架">测试签名</span>' : '';

    tr.innerHTML = `
      <td><div class="nm">${escapeHtml(j.appName || j.source || '-')}${testTag}</div>${err}</td>
      <td><div>${escapeHtml(j.versionName || '')} <span class="dim">(${escapeHtml(j.versionCode || '')})</span></div>
          <div class="dim">${escapeHtml(j.package || '')}</div></td>
      <td class="dim">${escapeHtml(fmtTime(j.createdAt))}</td>
      <td><span class="badge ${badge[0]}">${badge[1]}</span></td>
      <td><div class="dim meta">${meta.join(' · ')}</div><div class="rowbtns">${ops}</div></td>`;
    tr.onclick = (e) => {
      if (e.target.closest('button,a')) return;
      fillFromJob(j);
    };
    body.appendChild(tr);
  });

  body.querySelectorAll('[data-fast]').forEach((b) => {
    b.onclick = () => fastDownload(b.dataset.fast, b.dataset.name, +b.dataset.size);
  });
  body.querySelectorAll('[data-retry]').forEach((b) => {
    b.onclick = async () => {
      const r = await fetch(`/api/jobs/${b.dataset.retry}/retry`, { method: 'POST' });
      if (!r.ok) { alert('重试失败：' + await r.text()); return; }
      watchJob((await r.json()).id);
    };
  });
  body.querySelectorAll('[data-del]').forEach((b) => {
    b.onclick = async () => {
      if (!confirm('移入回收站？7 天内可恢复。')) return;
      const r = await fetch('/api/jobs/' + b.dataset.del, { method: 'DELETE' });
      if (!r.ok) { alert('删除失败：' + await r.text()); return; }
      loadJobs(); loadTrash();
    };
  });
}

// 点击记录 → 回填表单
function fillFromJob(j) {
  if (j.uploadId) {
    uploadId = j.uploadId;
    $('fSrc').value = j.source || '（复用上次资源包）';
    $('fileName').textContent = j.source || '';
  }
  $('fName').value = j.appName || '';
  $('fPkg').value = j.package || '';
  $('fVName').value = j.versionName || '';
  $('fVCode').value = j.versionCode || '';
  $('fAppId').value = j.appId || '';
  if (j.appKey) $('fAppKey').value = j.appKey;
  if (j.keystore && j.keystore !== 'auto') {
    const el = document.querySelector('input[name=ks][value=saved]');
    el.checked = true;
    el.dispatchEvent(new Event('change'));
    $('fKs').value = j.keystore;
  }
  validate();
  window.scrollTo({ top: 0, behavior: 'smooth' });
  $('submitNote').textContent = '已回填上次配置，改完版本号即可打包';
}

$('q').addEventListener('input', () => { query = $('q').value; visible = 15; renderJobs(); });
$('fStatus').addEventListener('change', () => { statusFilter = $('fStatus').value; visible = 15; renderJobs(); });
$('btnMore').onclick = () => { visible += 20; renderJobs(); };
$('thTime').onclick = () => { sortDesc = !sortDesc; renderJobs(); };

// --------------------------- 回收站 ---------------------------
let trashOpen = false;

async function loadTrash() {
  const list = await (await fetch('/api/trash')).json();
  $('trashCount').textContent = list.length ? `(${list.length})` : '';
  const box = $('trashList');
  box.innerHTML = '';
  if (!list.length) {
    box.innerHTML = '<div class="empty">回收站是空的</div>';
    return;
  }
  list.forEach((t) => {
    const row = document.createElement('div');
    row.className = 'trashrow';
    row.innerHTML = `
      <div>
        <div class="nm">${escapeHtml(t.appName || t.source || '-')}</div>
        <div class="dim">${escapeHtml(t.versionName || '')} (${escapeHtml(t.versionCode || '')}) · ${escapeHtml(t.package || '')}
          · 删除于 ${escapeHtml(fmtTime(t.deletedAt))}</div>
      </div>
      <div class="rowbtns">
        <button class="btn sm" data-restore="${t.id}">恢复</button>
        <button class="btn sm ghost" data-purge="${t.id}">彻底删除</button>
      </div>`;
    box.appendChild(row);
  });
  box.querySelectorAll('[data-restore]').forEach((b) => {
    b.onclick = async () => {
      const r = await fetch(`/api/trash/${b.dataset.restore}/restore`, { method: 'POST' });
      if (!r.ok) { alert(await r.text()); return; }
      loadTrash(); loadJobs();
    };
  });
  box.querySelectorAll('[data-purge]').forEach((b) => {
    b.onclick = async () => {
      if (!confirm('彻底删除后不可恢复，确定？')) return;
      await fetch('/api/trash/' + b.dataset.purge, { method: 'DELETE' });
      loadTrash();
    };
  });
}

$('btnTrash').onclick = async () => {
  trashOpen = !trashOpen;
  $('trashPanel').hidden = !trashOpen;
  if (trashOpen) await loadTrash();
};
$('btnTrashClose').onclick = () => { trashOpen = false; $('trashPanel').hidden = true; };

// --------------------------- 多线程加速下载 ---------------------------
async function fastDownload(jobId, name, size) {
  const box = $('dlMeta');
  box.hidden = false;
  const PARTS = 4;
  const url = `/d/${jobId}/${encodeURIComponent(name)}`;

  try {
    const head = await fetch(url, { method: 'HEAD' });
    const total = +(head.headers.get('Content-Length') || size || 0);
    if (!head.headers.get('Accept-Ranges') || !total) {
      box.textContent = '服务端不支持分段，已回退普通下载';
      location.href = url;
      return;
    }

    const chunk = Math.ceil(total / PARTS);
    const ranges = [];
    for (let i = 0; i < PARTS; i++) {
      const s = i * chunk;
      if (s >= total) break;
      ranges.push([s, Math.min(s + chunk - 1, total - 1)]);
    }

    box.innerHTML = `<div>加速下载中（${ranges.length} 线程）…</div><div class="bar"><i></i></div>`;
    const inner = box.querySelector('i');
    let done = 0;

    const bufs = await Promise.all(ranges.map(async ([s, e]) => {
      const r = await fetch(url, { headers: { Range: `bytes=${s}-${e}` } });
      const buf = new Uint8Array(await r.arrayBuffer());
      done += buf.length;
      inner.style.width = (done / total * 100).toFixed(1) + '%';
      return buf;
    }));

    const all = new Uint8Array(total);
    let off = 0;
    bufs.forEach((b) => { all.set(b, off); off += b.length; });

    const blob = new Blob([all], { type: 'application/vnd.android.package-archive' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = name;
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 10000);

    box.innerHTML = `<div style="color:var(--ok)">下载完成 ${fmtBytes(total)}</div>`;
    setTimeout(() => { box.hidden = true; }, 4000);
  } catch (e) {
    box.textContent = '加速下载失败，已回退普通下载：' + e.message;
    location.href = url;
  }
}

// --------------------------- 工具 ---------------------------
function fmtTime(s) {
  if (!s) return '-';
  const d = new Date(s);
  if (isNaN(d)) return String(s).slice(5, 16).replace('T', ' ');
  const p = (n) => String(n).padStart(2, '0');
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

function fmtBytes(n) {
  if (!n && n !== 0) return '-';
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB';
  return (n / 1024 / 1024 / 1024).toFixed(2) + ' GB';
}

function escapeHtml(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function friendlyError(raw) {
  const s = String(raw || '').trim();
  if (/找不到上传的资源|no such file|The system cannot find/.test(s)) return '上传资源不存在或已过期，请重新上传';
  if (/daemon disappeared|OOM|killed/i.test(s)) return '内存不足导致构建被中断，请调大 Docker 内存后重试';
  if (/appkey/i.test(s)) return 'appkey 未配置或与 appid 不匹配，请检查离线打包 appkey';
  if (/Gradle 编译失败|BUILD FAILED/i.test(s)) return '编译失败，请查看构建日志定位具体报错';
  if (/证书|keystore/i.test(s)) return '证书配置有误，请检查证书文件与口令';
  return s.length > 100 ? s.slice(-100) : s;
}

// --------------------------- 启动 ---------------------------
loadEnv();
loadRuntime();
loadKeystores();
loadJobs();
loadTrash();

setInterval(() => { if (!busy) loadRuntime(true); }, 5000);
setInterval(() => {
  const running = lastJobs.some((j) => j.status === 'running' || j.status === 'queued');
  if (running || !es) loadJobs();
}, 4000);

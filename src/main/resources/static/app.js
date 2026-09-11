/* SingBoxUI frontend: CRUD over /api */
let CFG = null, META = {inbounds:[], outbounds:[]};
let editing = null; // {section, original}

const $ = id => document.getElementById(id);
const api = async (m, url, body) => {
  const r = await fetch(url, {method: m, headers: {'Content-Type':'application/json'},
    body: body === undefined ? undefined : JSON.stringify(body)});
  const t = await r.text();
  try { return JSON.parse(t); } catch { return {error: t}; }
};
const toast = msg => { const t = $('toast'); t.textContent = msg; t.hidden = false;
  clearTimeout(t._h); t._h = setTimeout(() => t.hidden = true, 2600); };

/* ---------- field schemas (dot-paths into the JSON object) ---------- */
const F = (key,label,o={}) => ({key,label,...o});
const TLS = [
  {sec:'TLS'},
  F('tls.enabled','TLS enabled',{type:'check'}),
  F('tls.server_name','SNI (server_name)',{ph:'example.com'}),
  F('tls.insecure','insecure',{type:'check'}),
  F('tls.utls.fingerprint','uTLS fingerprint',{type:'select',opts:['','chrome','firefox','safari','ios','android','edge','360','qq']}),
  F('tls.reality.enabled','Reality',{type:'check'}),
  F('tls.reality.public_key','Reality public_key'),
  F('tls.reality.short_id','Reality short_id'),
  F('tls.alpn','ALPN (comma)',{parse:'csv',fmt:'csv'}),
];
const TRANSPORT = [
  {sec:'Transport'},
  F('transport.type','transport',{type:'select',opts:['','ws','grpc','httpupgrade','quic','http']}),
  F('transport.path','path',{ph:'/'}),
  F('transport.host','host'),
  F('transport.service_name','grpc service_name'),
];
const OUT_SCHEMAS = {
  vless:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('uuid','uuid'),
    F('flow','flow',{type:'select',opts:['','xtls-rprx-vision']}),F('network','network',{type:'select',opts:['tcp','ws','grpc','httpupgrade']}),F('packet_encoding','packet_encoding',{ph:'xudp'}),...TLS,...TRANSPORT],
  vmess:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('uuid','uuid'),F('alter_id','alter_id',{type:'num'}),
    F('security','security',{type:'select',opts:['auto','aes-128-gcm','chacha20-poly1305','none']}),F('network','network',{type:'select',opts:['tcp','ws','grpc','httpupgrade']}),...TLS,...TRANSPORT],
  trojan:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('password','password',{pw:1}),F('network','network',{type:'select',opts:['tcp','ws','grpc','httpupgrade']}),...TLS,...TRANSPORT],
  shadowsocks:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('method','method',{type:'select',opts:['aes-128-gcm','aes-256-gcm','chacha20-ietf-poly1305','2022-blake3-aes-128-gcm','2022-blake3-aes-256-gcm','2022-blake3-chacha20-poly1305']}),F('password','password',{pw:1}),F('network','network',{type:'select',opts:['tcp','tcp,udp','udp']})],
  wireguard:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('private_key','private_key',{pw:1}),F('peer_public_key','peer_public_key'),F('pre_shared_key','pre-shared key',{pw:1}),F('local_address','local_address (comma)',{parse:'csv',fmt:'csv'}),F('mtu','mtu',{type:'num'})],
  hysteria2:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('password','password',{pw:1}),F('tls.server_name','SNI'),F('tls.insecure','insecure',{type:'check'}),F('obfs.type','obfs',{ph:'salamander'}),F('obfs.password','obfs password',{pw:1})],
  hysteria:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('auth_str','auth_str'),F('tls.server_name','SNI'),F('tls.insecure','insecure',{type:'check'})],
  tuic:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('uuid','uuid'),F('password','password',{pw:1}),F('tls.server_name','SNI'),F('tls.insecure','insecure',{type:'check'})],
  anytls:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('password','password',{pw:1}),F('tls.server_name','SNI'),F('tls.insecure','insecure',{type:'check'})],
  socks:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('username','username'),F('password','password',{pw:1}),F('version','version',{type:'select',opts:['5','4a','4']})],
  http:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('username','username'),F('password','password',{pw:1}),F('tls.enabled','TLS',{type:'check'}),F('tls.server_name','SNI')],
  ssh:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('user','user'),F('password','password',{pw:1}),F('private_key','private_key')],
  tor:[F('tag','tag'),F('executable_path','tor path',{ph:'/usr/bin/tor'}),F('data_directory','data dir')],
  dns:[F('tag','tag')],
  direct:[F('tag','tag'),F('override_address','override_address'),F('override_port','override_port',{type:'num'})],
  block:[F('tag','tag')],
  selector:[F('tag','tag'),F('outbounds','members (comma)',{parse:'csv',fmt:'csv'}),F('default','default outbound'),F('interrupt_exist_connections','interrupt existing',{type:'check'})],
  urltest:[F('tag','tag'),F('outbounds','members (comma)',{parse:'csv',fmt:'csv'}),F('url','url',{ph:'https://www.gstatic.com/generate_204'}),F('interval','interval',{ph:'1m'}),F('tolerance','tolerance',{type:'num'})],
  snell:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('psk','psk',{pw:1}),F('version','version',{type:'select',opts:['2','3','4']})],
  naive:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('username','username'),F('password','password',{pw:1}),F('tls.server_name','SNI')],
  shadowtls:[F('tag','tag'),F('server','server'),F('server_port','port',{type:'num'}),F('password','password',{pw:1}),F('tls.server_name','SNI')],
};
const IN_SCHEMAS = {
  tun:[F('tag','tag'),F('interface_name','interface (macOS: пусто=авто, иначе utun4; Linux/Win: tun0)',{ph:'auto'}),F('address','address (comma)',{parse:'csv',fmt:'csv',ph:'172.19.0.1/30'}),F('mtu','mtu',{type:'num'}),F('stack','stack',{type:'select',opts:['system','gvisor','mixed']}),F('auto_route','auto_route',{type:'check'}),F('auto_redirect','auto_redirect',{type:'check'}),F('strict_route','strict_route',{type:'check'})],
  mixed:[F('tag','tag'),F('listen','listen',{ph:'127.0.0.1'}),F('listen_port','port',{type:'num'}),F('sniff','sniff',{type:'check'})],
  socks:[F('tag','tag'),F('listen','listen',{ph:'127.0.0.1'}),F('listen_port','port',{type:'num'}),F('sniff','sniff',{type:'check'})],
  http:[F('tag','tag'),F('listen','listen',{ph:'127.0.0.1'}),F('listen_port','port',{type:'num'}),F('sniff','sniff',{type:'check'})],
  direct:[F('tag','tag'),F('listen','listen'),F('listen_port','port',{type:'num'})],
  tproxy:[F('tag','tag'),F('listen','listen',{ph:'0.0.0.0'}),F('listen_port','port',{type:'num'}),F('network','network',{type:'select',opts:['tcp','udp','tcp,udp']})],
  redirect:[F('tag','tag'),F('listen','listen',{ph:'0.0.0.0'}),F('listen_port','port',{type:'num'})],
  shadowsocks:[F('tag','tag'),F('listen','listen'),F('listen_port','port',{type:'num'}),F('method','method'),F('password','password',{pw:1})],
  vmess:[F('tag','tag'),F('listen','listen'),F('listen_port','port',{type:'num'})],
  vless:[F('tag','tag'),F('listen','listen'),F('listen_port','port',{type:'num'})],
  trojan:[F('tag','tag'),F('listen','listen'),F('listen_port','port',{type:'num'})],
  naive:[F('tag','tag'),F('listen','listen'),F('listen_port','port',{type:'num'})],
  hysteria2:[F('tag','tag'),F('listen','listen'),F('listen_port','port',{type:'num'})],
  tuic:[F('tag','tag'),F('listen','listen'),F('listen_port','port',{type:'num'})],
  anytls:[F('tag','tag'),F('listen','listen'),F('listen_port','port',{type:'num'})],
};

/* ---------- dot-path helpers ---------- */
const get = (o,p) => p.split('.').reduce((a,k)=>a?.[k], o);
const set = (o,p,v) => { const ks=p.split('.'); let t=o;
  ks.forEach((k,i)=>{ if(i===ks.length-1){ v===undefined||v==='' ? delete t[k] : t[k]=v; }
    else { if(typeof t[k]!=='object'||!t[k]) t[k]={}; t=t[k]; } }); };
const prune = o => { if(Array.isArray(o)){o.forEach(prune);return o;}
  if(o&&typeof o==='object'){for(const k of Object.keys(o)){ if(o[k]&&typeof o[k]==='object'){prune(o[k]);
    if(!Object.keys(o[k]).length) delete o[k];} else if(o[k]===''||o[k]===null) delete o[k];}} return o; };

/* ---------- render ---------- */
async function load() {
  META = await api('GET','/api/meta');
  if (META.tunDefault !== undefined) {
    const f = IN_SCHEMAS.tun.find(f=>f.key==='interface_name');
    if (f) f.ph = META.tunDefault || 'auto (utunN)';
  }
  fillTypes($('newOutType'), META.outbounds || Object.keys(OUT_SCHEMAS), 'vless');
  fillTypes($('newInType'), META.inbounds || Object.keys(IN_SCHEMAS), 'tun');
  await refresh();
}
function fillTypes(sel, arr, def){ sel.innerHTML = arr.map(t=>`<option ${t===def?'selected':''}>${t}</option>`).join(''); }

async function refresh() {
  CFG = await api('GET','/api/config');
  renderOut(); renderIn(); renderRoute(); renderDns(); renderStatus();
}
async function renderStatus() {
  const s = await api('GET','/api/status');
  const p = $('statusPill');
  p.textContent = (s.found ? 'sing-box ' + s.version : 'без бинарника') + ' · ' + (s.valid ? 'валидно' : 'ошибки: ' + s.errors.length);
  p.dataset.base = p.textContent;
  p.className = 'pill ' + (s.valid ? 'ok' : 'bad');
  p.title = (s.errors||[]).join('\n') || s.configPath;
  renderProc();
}

/* ---------- process control ---------- */
let procTimer = null;
async function renderProc() {
  const sb = await api('GET','/api/singbox');
  const bs = $('sbState');
  const srcName = {managed:'встроенный', PATH:'из PATH', settings:'свой', missing:'нет'}[sb.source] || sb.source;
  bs.textContent = sb.found ? `${sb.version} · ${srcName}` : 'sing-box не найден';
  bs.className = 'pill ' + (sb.found ? 'ok' : 'bad');
  $('sbPath').textContent = sb.found ? sb.binary : (sb.note || 'нажми «Скачать sing-box» — всё сделается само');
  $('btnSbInstall').textContent = sb.found ? 'Обновить sing-box' : 'Скачать sing-box';
  const s = await api('GET','/api/proc/status');
  const st = $('procState');
  st.textContent = s.running ? `● запущен (pid ${s.pid})` : '○ остановлен';
  st.className = 'pill ' + (s.running ? 'ok' : '');
  $('procInfo').textContent = s.running ? `uptime ${s.uptimeSec}s · ${s.configPath}` : `last exit: ${s.lastExit} · ${s.binary}`;
  $('procCmd').textContent = `${s.binary} run -c ${s.configPath}`;
  $('btnProcStart').disabled = s.running;
  const pill = $('statusPill');
  pill.textContent = (pill.dataset.base || pill.textContent) + (s.running ? ' · запущен' : '');
}
async function renderProcLogs() {
  const r = await api('GET','/api/proc/logs?lines=200');
  const pre = $('procLogs');
  pre.textContent = (r.logs||[]).join('\n') || 'Лог пуст.';
  if ($('procFollow').checked) pre.scrollTop = pre.scrollHeight;
}
$('btnProcStart').onclick = async () => {
  const r = await api('POST','/api/proc/start');
  if (r.error) toast('Не запустился: ' + r.error); else toast('sing-box запущен (pid ' + r.pid + ')');
  renderProc(); renderProcLogs();
};
$('btnProcStop').onclick = async () => { await api('POST','/api/proc/stop'); renderProc(); renderProcLogs(); toast('Остановлен'); };
$('btnProcRestart').onclick = async () => {
  const r = await api('POST','/api/proc/restart');
  if (r.error) toast('Не перезапустился: ' + r.error); else toast('Перезапущен (pid ' + r.pid + ')');
  renderProc(); renderProcLogs();
};
$('btnProcLogs').onclick = renderProcLogs;
$('btnSbInstall').onclick = async () => {
  const b = $('btnSbInstall');
  b.disabled = true; b.textContent = 'Качаю…';
  const r = await api('POST','/api/singbox/install');
  b.disabled = false;
  if (r.error) toast('Не скачался: ' + r.error); else toast('sing-box готов: ' + r.version);
  renderStatus();
};

function summary(o) {
  const t = o.type;
  if (o.server) return `${o.server}:${o.server_port ?? ''}`.replace(/:$/,'');
  if (t==='selector'||t==='urltest') return (o.outbounds||[]).join(', ');
  if (t==='mixed'||t==='socks'||t==='http') return `${o.listen||'0.0.0.0'}:${o.listen_port??''}`;
  if (t==='tun') return (o.address||[]).join(', ') + (o.auto_route?' · auto_route':'');
  return o.tag || '';
}

function card(o, section) {
  const d = document.createElement('div'); d.className = 'card';
  d.innerHTML = `<div class="top"><span class="badge">${o.type}</span><span class="tag">${o.tag||''}</span></div>
    <div class="addr">${summary(o)||'—'}</div>
    <div class="ops"><button data-a="edit">Изменить</button>
    <button data-a="json" class="ghost">{ }</button>
    ${section==='outbounds'&&['vless','vmess','trojan','shadowsocks','hysteria2','tuic'].includes(o.type)?'<button data-a="share">Ссылка</button>':''}
    <button data-a="del" class="ghost">Удалить</button></div>`;
  d.querySelector('[data-a=edit]').onclick = () => openModal(section, o);
  d.querySelector('[data-a=json]').onclick = () => showJson(o.type + ' ' + (o.tag||''), o);
  const del = d.querySelector('[data-a=del]');
  del.onclick = async () => { if(!confirm('Удалить '+o.tag+'?')) return;
    await api('DELETE', `/api/${section}/${encodeURIComponent(o.tag)}`); refresh(); };
  const sh = d.querySelector('[data-a=share]');
  if (sh) sh.onclick = async () => { const r = await api('GET', `/api/share/export/${encodeURIComponent(o.tag)}`);
    if (r.link) { await navigator.clipboard.writeText(r.link).catch(()=>prompt('Скопируй ссылку:', r.link)); toast('Ссылка скопирована'); }
    else toast(r.error || 'не получилось'); };
  return d;
}

function renderOut() {
  const list = $('outList'); list.innerHTML = '';
  $('cntOut').textContent = (CFG.outbounds||[]).length;
  (CFG.outbounds||[]).forEach(o => list.appendChild(card(o,'outbounds')));
}
function renderIn() {
  const list = $('inList'); list.innerHTML = '';
  $('cntIn').textContent = (CFG.inbounds||[]).length;
  (CFG.inbounds||[]).forEach(o => list.appendChild(card(o,'inbounds')));
}

function renderRoute() {
  const tags = (CFG.outbounds||[]).map(o=>o.tag);
  $('routeFinal').innerHTML = tags.map(t=>`<option ${CFG.route?.final===t?'selected':''}>${t}</option>`).join('');
  $('routeAutoIf').checked = !!CFG.route?.auto_detect_interface;
  const rl = $('ruleList'); rl.innerHTML = '';
  (CFG.route?.rules||[]).forEach((r,i) => {
    const d = document.createElement('div'); d.className='rule';
    const out = r.outbound || r.action || '';
    const cls = out==='direct' ? 'b-direct' : (out==='block'||out==='reject'||r.action==='reject') ? 'b-block' : 'b-proxy';
    d.innerHTML = `<b>#${i}</b> <span>${escapeHtml(describeRule(r))}</span>
      <span class="ob ${cls}">→ ${escapeHtml(destLabel(r))}</span>
      <button data-a="e">Править</button><button data-a="j" class="ghost">{ }</button><button data-a="d" class="ghost">✕</button>`;
    d.querySelector('[data-a=d]').onclick = async () => { await api('DELETE',`/api/route/rules/${i}`); refresh(); };
    d.querySelector('[data-a=e]').onclick = () => openRuleModal(r,i);
    d.querySelector('[data-a=j]').onclick = () => showJson('Правило route #' + i, r);
    rl.appendChild(d);
  });
}

function describeRule(r) {
  const j = v => Array.isArray(v) ? v.join(', ') : String(v ?? '');
  const p = [];
  if (r.domain_suffix || r.domain) p.push('Домены: ' + j(r.domain_suffix || r.domain));
  if (r.domain_keyword) p.push('Содержат: ' + j(r.domain_keyword));
  if (r.domain_regex) p.push('Regex: ' + j(r.domain_regex));
  if (r.ip_cidr) p.push('IP: ' + j(r.ip_cidr));
  if (r.rule_set) p.push('Наборы: ' + j(r.rule_set));
  if (r.process_name) p.push('Процессы: ' + j(r.process_name));
  if (r.port) p.push('Порты: ' + j(r.port));
  if (r.port_range) p.push('Диапазон: ' + j(r.port_range));
  if (r.protocol) p.push('Протокол: ' + j(r.protocol));
  if (r.inbound) p.push('Вход: ' + j(r.inbound));
  if (r.ip_is_private) p.push('Приватные IP');
  if (!p.length) p.push('Весь остальной трафик');
  return p.join(' · ');
}
function destLabel(r) {
  if (r.outbound) return r.outbound;
  return r.action || '?';
}

/* Split-tunnel rule wizard */
const SPLIT_KINDS = {
  domain:  {label:'Домены', ph:'youtube.com, discord.com, ntv.ru', hint:'Домены и их поддомены (domain_suffix).'},
  ip:      {label:'IP и подсети', ph:'1.2.3.4, 10.0.0.0/8', hint:'IP-адреса и CIDR (ip_cidr).'},
  geo:     {label:'Гео-наборы', ph:'geosite-ru, geoip-ru', hint:'Наборы sing-box. geosite-*/geoip-* подтянутся автоматически как remote rule-set.'},
  process: {label:'Программы', ph:'chrome.exe, Discord', hint:'Имена процессов (работает там, где sing-box их видит).'},
  port:    {label:'Порты', ph:'80, 443', hint:'Порты назначения.'},
  protocol:{label:'Протоколы', ph:'dns, tls, http', hint:'Протокол после sniff (dns, http, tls, quic, …).'},
};
function openSplitModal() {
  const tags = (CFG.outbounds||[]).map(o=>o.tag);
  const guess = tags.find(t=>!['direct','block','dns','dns-out'].includes(t)) || tags[0] || 'direct';
  $('mTitle').textContent = 'Новое правило — куда направить трафик';
  $('mForm').innerHTML = `
    <label class="field"><span>Что выделяем</span><select id="spKind">${
      Object.entries(SPLIT_KINDS).map(([k,v])=>`<option value="${k}">${v.label}</option>`).join('')
    }</select></label>
    <label class="field"><span>Направить через</span><select id="spOut">${
      tags.map(t=>`<option ${t===guess?'selected':''}>${t}</option>`).join('')
    }</select></label>
    <label class="field full"><span>Значения (через запятую или с новой строки)</span>
      <input id="spValues" placeholder="${SPLIT_KINDS.domain.ph}"></label>
    <div class="muted full" id="spHint">${SPLIT_KINDS.domain.hint}</div>`;
  $('spKind').onchange = e => {
    $('spValues').placeholder = SPLIT_KINDS[e.target.value].ph;
    $('spHint').textContent = SPLIT_KINDS[e.target.value].hint;
  };
  $('modal').hidden = false;
  $('mSave').onclick = async () => {
    const r = await api('POST','/api/split/rules',{kind:$('spKind').value,
      values:$('spValues').value, outbound:$('spOut').value});
    if (r.error) { toast('Ошибка: '+r.error); return; }
    $('modal').hidden = true; bindSave(); refresh(); toast('Правило добавлено');
  };
}

function renderDns() {
  $('dnsStrategy').value = CFG.dns?.strategy || '';
  $('dnsFinal').value = CFG.dns?.final || '';
  const dl = $('dnsList'); dl.innerHTML = '';
  (CFG.dns?.servers||[]).forEach(s => {
    const d = document.createElement('div'); d.className='dnsrow';
    d.innerHTML = `<span class="badge">${s.type||'?'}</span><b>${s.tag||''}</b><code>${escapeHtml(s.server||JSON.stringify(s))}</code>
      <button data-a="e">Править</button><button data-a="j" class="ghost">{ }</button><button data-a="d" class="ghost">✕</button>`;
    d.querySelector('[data-a=d]').onclick = async () => { await api('DELETE',`/api/dns/servers/${encodeURIComponent(s.tag)}`); refresh(); };
    d.querySelector('[data-a=e]').onclick = () => openDnsModal(s);
    d.querySelector('[data-a=j]').onclick = () => showJson('DNS ' + (s.tag||''), s);
    dl.appendChild(d);
  });
}

const escapeHtml = s => s.replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));

/* ---------- JSON viewer ---------- */
function showJson(title, obj) {
  $('mTitle').textContent = title;
  const txt = JSON.stringify(obj, null, 2);
  $('mForm').innerHTML = `<pre class="jsonview full">${escapeHtml(txt)}</pre>`;
  $('modal').hidden = false;
  const save = $('mSave');
  save.textContent = 'Копировать';
  save.onclick = async () => {
    try { await navigator.clipboard.writeText(txt); toast('JSON скопирован'); }
    catch { toast('Не скопировалось — выдели вручную'); }
    $('modal').hidden = true; save.textContent = 'Сохранить'; bindSave();
  };
  $('mCancel').onclick = () => { $('modal').hidden = true; save.textContent = 'Сохранить'; bindSave(); };
}

/* ---------- traffic monitor ---------- */
const fmtBytes = n => {
  if (!n) return '0 B';
  const u = ['B','KB','MB','GB','TB'];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return n.toFixed(n >= 100 ? 0 : 1) + ' ' + u[i];
};
async function renderTraffic() {
  try {
    const t = await api('GET','/api/traffic');
    if (!t.available) {
      $('trState').textContent = 'трафик: sing-box не запущен';
      $('trTotal').textContent = '';
      $('trVpn').textContent = '';
      $('trDirect').textContent = '';
      return;
    }
    $('trState').textContent = `трафик (${t.conns} соед.):`;
    $('trTotal').textContent = `▼ ${fmtBytes(t.down)} ▲ ${fmtBytes(t.up)}`;
    $('trVpn').textContent = `VPN ▼ ${fmtBytes(t.vpn.down)} ▲ ${fmtBytes(t.vpn.up)}`;
    $('trDirect').textContent = `direct ▼ ${fmtBytes(t.direct.down)} ▲ ${fmtBytes(t.direct.up)}`;
  } catch { /* ignore */ }
}
setInterval(renderTraffic, 2000);

/* ---------- modal ---------- */
function openModal(section, obj) {
  const type = obj?.type || (section==='outbounds' ? $('newOutType').value : $('newInType').value);
  const schema = (section==='outbounds' ? OUT_SCHEMAS : IN_SCHEMAS)[type] || [F('tag','tag')];
  editing = {section, original: obj ? JSON.parse(JSON.stringify(obj)) : null};
  $('mTitle').textContent = (obj?'Изменить ':'Новый ') + type + ' (' + section + ')';
  const form = $('mForm'); form.innerHTML = '';
  form.appendChild(hiddenType(type));
  schema.forEach(f => {
    if (f.sec) { const h=document.createElement('div'); h.className='fsec'; h.textContent=f.sec; form.appendChild(h); return; }
    form.appendChild(fieldEl(f, obj ? get(obj,f.key) : defVal(f)));
  });
  $('modal').hidden = false;
}
const hiddenType = t => { const i=document.createElement('input'); i.type='hidden'; i.id='f_type'; i.value=t; return i; };
function defVal(f){ if(f.type==='check') return false; if(f.key==='server_port') return 443; return ''; }

function fieldEl(f, val) {
  const w = document.createElement('label'); w.className='field';
  w.innerHTML = `<span>${f.label}</span>`;
  let inp;
  if (f.type==='check') { inp=document.createElement('input'); inp.type='checkbox'; inp.checked=!!val; }
  else if (f.type==='select') { inp=document.createElement('select');
    inp.innerHTML = f.opts.map(o=>`<option value="${o}" ${String(val??'')===o?'selected':''}>${o||'—'}</option>`).join(''); }
  else if (f.type==='num') { inp=document.createElement('input'); inp.type='number'; inp.value=val??''; }
  else { inp=document.createElement('input'); inp.type = f.pw?'password':'text'; inp.value = fmtVal(f,val);
    if(f.ph) inp.placeholder=f.ph; }
  inp.dataset.key=f.key; inp.dataset.parse=f.parse||'';
  w.appendChild(inp); return w;
}
const fmtVal = (f,v) => f.parse==='csv'&&Array.isArray(v) ? v.join(', ') : (v??'');
const parseVal = inp => {
  if (inp.type==='checkbox') return inp.checked || undefined;
  if (inp.type==='number') return inp.value==='' ? undefined : +inp.value;
  if (inp.dataset.parse==='csv') return inp.value.split(',').map(s=>s.trim()).filter(Boolean);
  return inp.value.trim() === '' ? undefined : inp.value.trim();
};

$('mCancel').onclick = () => $('modal').hidden = true;
$('modal').addEventListener('click', e => { if (e.target.id==='modal') $('modal').hidden = true; });
$('mSave').onclick = async () => {
  const type = $('f_type').value;
  const base = editing.original ? JSON.parse(JSON.stringify(editing.original)) : {type};
  base.type = type;
  document.querySelectorAll('#mForm [data-key]').forEach(inp => set(base, inp.dataset.key, parseVal(inp)));
  prune(base);
  const r = await api('POST', `/api/${editing.section}`, base);
  if (r.error) { toast('Ошибка: '+r.error); return; }
  $('modal').hidden = true; refresh(); toast('Сохранено: '+(r.tag||type));
};

/* ---------- route rule modal (generic key=value) ---------- */
function openRuleModal(rule, idx) {
  editing = {section:'__rule', index: idx};
  $('mTitle').textContent = idx===undefined ? 'Новое правило route' : 'Правило route #'+idx;
  const form = $('mForm'); form.innerHTML='';
  const ta = document.createElement('textarea'); ta.id='ruleTa'; ta.className='full';
  ta.style.minHeight='220px'; ta.value = JSON.stringify(rule||{action:'route',outbound:'direct'},null,2);
  form.appendChild(ta); $('modal').hidden=false;
  $('mSave').onclick = async () => {
    let r; try { r = JSON.parse($('ruleTa').value); } catch(e){ toast('Невалидный JSON'); return; }
    if (idx!==undefined) {
      const route = JSON.parse(JSON.stringify(CFG.route||{}));
      route.rules[idx]=r; await api('POST','/api/section/route',route);
    } else await api('POST','/api/route/rules',r);
    $('modal').hidden=true; bindSave(); refresh();
  };
}
function openDnsModal(s) {
  editing = {section:'__dns'};
  $('mTitle').textContent = s ? 'DNS сервер '+s.tag : 'Новый DNS сервер';
  const form = $('mForm'); form.innerHTML='';
  const mk = (k,v,ph='') => `<label class="field"><span>${k}</span><input data-k="${k}" value="${(v??'').toString().replaceAll('"','&quot;')}" placeholder="${ph}"></label>`;
  form.innerHTML = mk('tag',s?.tag)+mk('type',s?.type,'tls')+mk('server',s?.server,'8.8.8.8')
    + `<label class="field"><span>server_port (DoT/DoQ)</span><input data-k="server_port" value="${s?.server_port??''}"></label>`
    + `<label class="field full"><span>path (DoH)</span><input data-k="path" value="${s?.path??''}"></label>`;
  $('modal').hidden=false;
  $('mSave').onclick = async () => {
    const o={}; document.querySelectorAll('#mForm [data-k]').forEach(i=>{ if(i.value!=='') o[i.dataset.k]=i.value; });
    if(o.server_port) o.server_port=+o.server_port;
    await api('POST','/api/dns/servers',o); $('modal').hidden=true; bindSave(); refresh();
  };
}
function bindSave(){ $('mSave').onclick = saveMain; }
const saveMain = async () => {
  const type = $('f_type').value;
  const base = editing.original ? JSON.parse(JSON.stringify(editing.original)) : {type};
  base.type = type;
  document.querySelectorAll('#mForm [data-key]').forEach(inp => set(base, inp.dataset.key, parseVal(inp)));
  prune(base);
  const r = await api('POST', `/api/${editing.section}`, base);
  if (r.error) { toast('Ошибка: '+r.error); return; }
  $('modal').hidden = true; refresh(); toast('Сохранено: '+(r.tag||type));
};

/* ---------- toolbar wiring ---------- */
document.querySelectorAll('.tabs button').forEach(b => b.onclick = () => {
  document.querySelectorAll('.tabs button').forEach(x=>x.classList.remove('active'));
  b.classList.add('active');
  document.querySelectorAll('.tab').forEach(t=>t.hidden=true);
  $('tab-'+b.dataset.tab).hidden=false;
  if (procTimer) { clearInterval(procTimer); procTimer = null; }
  if (b.dataset.tab==='raw') rawLoad();
  if (b.dataset.tab==='run') { renderProc(); renderProcLogs(); procTimer = setInterval(() => { renderProc(); renderProcLogs(); }, 3000); }
});
$('btnAddOut').onclick = () => { bindSave(); openModal('outbounds', null); };
$('btnAddIn').onclick = () => { bindSave(); openModal('inbounds', null); };
$('btnShareImport').onclick = async () => {
  const link = $('shareIn').value.trim(); if(!link) return;
  const r = await api('POST','/api/share/import',{link});
  if (r.error) toast('Ошибка: '+r.error); else { toast('Импортирован: '+r.tag); $('shareIn').value=''; refresh(); }
};
$('btnSaveRoute').onclick = async () => {
  const route = JSON.parse(JSON.stringify(CFG.route||{}));
  route.final = $('routeFinal').value; route.auto_detect_interface = $('routeAutoIf').checked;
  await api('POST','/api/section/route',route); refresh(); toast('Route сохранён');
};
$('btnAddSplit').onclick = () => openSplitModal();
$('btnSaveDns').onclick = async () => {
  const dns = JSON.parse(JSON.stringify(CFG.dns||{}));
  dns.strategy = $('dnsStrategy').value || undefined; dns.final = $('dnsFinal').value || undefined;
  prune(dns); await api('POST','/api/section/dns',dns); refresh(); toast('DNS сохранён');
};
$('btnAddDns').onclick = () => openDnsModal(null);
$('btnCheck').onclick = async () => {
  const r = await api('POST','/api/check');
  const box = $('checkOut'); box.hidden=false;
  box.textContent = (r.ok?'✔ ':'✘ ') + (r.output||'');
  renderStatus();
};
$('btnExport').onclick = () => window.location = '/api/export';
$('btnFileImport').onclick = () => $('fileImport').click();
async function importConfigFile(file, {apply = true} = {}) {
  if (!file) return null;
  let parsed;
  try { parsed = JSON.parse(await file.text()); }
  catch { toast('Невалидный JSON в файле ' + file.name); return null; }
  if (!apply) return parsed;
  const r = await api('POST','/api/import', parsed);
  if (r.error) toast('Ошибка: ' + r.error);
  else { refresh(); toast('Конфиг импортирован из ' + file.name); }
  return r;
}
$('fileImport').onchange = async e => {
  await importConfigFile(e.target.files[0]);
  e.target.value = '';
};
$('tplSelect').onchange = async e => {
  if(!e.target.value) return;
  if(!confirm('Заменить текущий конфиг шаблоном "'+e.target.value+'"?')) { e.target.value=''; return; }
  await api('POST','/api/config/template/'+e.target.value); e.target.value=''; refresh();
};

/* ---------- raw ---------- */
async function rawLoad(){ $('rawJson').value = JSON.stringify(CFG||await api('GET','/api/config'),null,2); $('rawMsg').textContent=''; }
$('btnRawLoad').onclick = rawLoad;
$('btnRawFile').onclick = () => $('rawFile').click();
$('rawFile').onchange = async e => {
  const parsed = await importConfigFile(e.target.files[0], {apply:false});
  e.target.value = '';
  if (parsed) { $('rawJson').value = JSON.stringify(parsed, null, 2); $('rawMsg').textContent = 'Загружено из файла — проверь и нажми Сохранить'; }
};
$('btnRawDownload').onclick = () => window.location = '/api/export';
const rawTa = $('rawJson');
rawTa.addEventListener('dragover', e => { e.preventDefault(); rawTa.style.borderColor = '#2f81f7'; });
rawTa.addEventListener('dragleave', () => rawTa.style.borderColor = '');
rawTa.addEventListener('drop', async e => {
  e.preventDefault(); rawTa.style.borderColor = '';
  const f = e.dataTransfer.files && e.dataTransfer.files[0];
  const parsed = await importConfigFile(f, {apply:false});
  if (parsed) { rawTa.value = JSON.stringify(parsed, null, 2); $('rawMsg').textContent = 'Загружено из файла — проверь и нажми Сохранить'; }
});
$('btnRawValidate').onclick = async () => {
  try { JSON.parse($('rawJson').value); } catch(e){ $('rawMsg').textContent='JSON ошибка: '+e.message; return; }
  const r = await api('POST','/api/config', JSON.parse($('rawJson').value));
  $('rawMsg').textContent = r.error ? 'Структурные ошибки: '+r.error : 'Структурно валидно. Нажми Check для sing-box check.';
  refresh();
};
$('btnRawSave').onclick = async () => {
  try { const r = await api('POST','/api/config', JSON.parse($('rawJson').value));
    $('rawMsg').textContent = r.error ? 'Ошибка: '+r.error : 'Сохранено';
    if(!r.error) refresh(); } catch(e){ $('rawMsg').textContent='JSON ошибка: '+e.message; }
};

load();

const $ = s => document.querySelector(s);
const $$ = s => [...document.querySelectorAll(s)];
const TOKEN = (() => { const p = new URLSearchParams(location.hash.slice(1)); const t = p.get('token') || sessionStorage.getItem('w11-token') || ''; if (t) sessionStorage.setItem('w11-token', t); history.replaceState(null, '', location.pathname); return t; })();

const categories = {
  all:        { icon:'◈', title:'Все настройки', nav:'Обзор', desc:'Безопасные изменения Windows 11' },
  appearance: { icon:'✦', title:'Оформление', nav:'Оформление', desc:'Тема, цвет и визуальные эффекты' },
  taskbar:    { icon:'▤', title:'Панель задач', nav:'Панель задач', desc:'Пуск, поиск, виджеты и команды' },
  explorer:   { icon:'▱', title:'Проводник', nav:'Проводник', desc:'Файлы, меню и плотность интерфейса' },
  privacy:    { icon:'◉', title:'Приватность', nav:'Приватность', desc:'Реклама, рекомендации и история действий' },
  gaming:     { icon:'⌁', title:'Игры', nav:'Игры', desc:'Игровой режим, запись и управление мышью' },
  system:     { icon:'⚙', title:'Система', nav:'Система', desc:'Поведение интерфейса и совместимость' }
};
const cardIcons = {appearance:'✦',taskbar:'▤',explorer:'▱',privacy:'◉',gaming:'⌁',system:'⚙'};
let state = null, activeCategory = 'all', filter = 'all', query = '', busy = false;

async function api(path, opts={}) {
  const res = await fetch('/api/' + path, { ...opts, headers:{'X-Customizer-Token':TOKEN,'Content-Type':'application/json',...(opts.headers||{})} });
  let data = {}; try { data = await res.json(); } catch {}
  if (!res.ok) throw new Error(data.error || `Ошибка ${res.status}`);
  return data;
}

function initNav() {
  $('#nav').innerHTML = Object.entries(categories).map(([id,c]) => `<button class="nav-item ${id==='all'?'active':''}" data-cat="${id}" aria-label="${c.nav}"><span class="ico">${c.icon}</span><span class="nav-label">${c.nav}</span></button>`).join('');
  $$('.nav-item').forEach(b => b.onclick = () => { activeCategory=b.dataset.cat; $$('.nav-item').forEach(x=>x.classList.toggle('active',x===b)); updateHeading(); render(); $('#workspace').scrollIntoView({behavior:'smooth',block:'start'}); });
}

function updateClock() {
  const now = new Date();
  $('#clock').textContent = now.toLocaleTimeString('ru-RU', {hour:'2-digit', minute:'2-digit'});
  $('#date').textContent = now.toLocaleDateString('ru-RU', {weekday:'long', day:'numeric', month:'long'});
}

function updateHeading() {
  const c = categories[activeCategory];
  $('#pageTitle').textContent = c.nav;
  $('#sectionTitle').textContent = c.title;
  $('#sectionDesc').textContent = c.desc;
  $('#eyebrow').textContent = activeCategory === 'all' ? 'ОБЗОР' : 'НАСТРОЙКИ';
}

async function load() {
  try {
    state = await api('state');
    $('#version').textContent = `Версия ${state.version}`;
    $('#adminState').textContent = state.admin ? 'Администратор' : 'Обычный режим';
    $('.status-dot').style.background = state.admin ? 'var(--green)' : '#f6bd55';
    $('#backupText').textContent = state.backupCount ? `Сохранено исходных значений: ${state.backupCount}` : 'Резервная копия пуста';
    const active = state.tweaks.filter(x=>x.enabled).length;
    $('#score').textContent = active;
    render();
  } catch(e) { $('#grid').innerHTML = `<div class="empty">Не удалось прочитать параметры: ${esc(e.message)}</div>`; toast(e.message,true); }
}

function visibleTweaks() {
  if (!state) return [];
  return state.tweaks.filter(t => (activeCategory==='all'||t.category===activeCategory) && (filter==='all'||filter==='recommended'&&t.recommended||filter==='enabled'&&t.enabled) && (!query||`${t.title} ${t.description}`.toLowerCase().includes(query)));
}

function render() {
  if (!state) return;
  const list = visibleTweaks();
  $('#count').textContent = `${list.length} из ${state.tweaks.length}`;
  $('#grid').innerHTML = list.length ? list.map(t => `<article class="card" title="${esc(t.description)}">
    <div class="card-icon">${cardIcons[t.category]||'•'}</div>
    <div class="card-body"><div class="card-title"><b>${esc(t.title)}</b>${t.recommended?'<span class="tag">СОВЕТ</span>':''}${t.risk==='medium'?'<span class="tag medium">ОСТОРОЖНО</span>':''}${t.admin?'<span class="tag admin">ADMIN</span>':''}</div><p>${esc(t.description)}</p></div>
    <label class="switch"><input type="checkbox" data-id="${t.id}" ${t.enabled?'checked':''} ${busy?'disabled':''}><span class="slider"></span></label>
  </article>`).join('') : '<div class="empty">Настройки не найдены</div>';
  $$('.switch input').forEach(input => input.onchange = () => requestToggle(input));
}

async function requestToggle(input) {
  const t = state.tweaks.find(x=>x.id===input.dataset.id), enabled=input.checked;
  input.checked = !enabled;
  const proceed = async () => {
    setBusy(true);
    try { const r=await api('apply',{method:'POST',body:JSON.stringify({id:t.id,enabled})}); toast(`${t.title}: ${enabled?'включено':'выключено'}${restartText(r.restart)}`); await load(); }
    catch(e){ toast(e.message,true); }
    finally{ setBusy(false); }
  };
  if (t.risk === 'medium') confirmBox(t.title, `${t.description} Изменение можно откатить.`, proceed); else proceed();
}

function restartText(v){ return v==='explorer'?' · нужен перезапуск Проводника':v==='signout'?' · потребуется выход из системы':v==='reboot'?' · потребуется перезагрузка':''; }
function setBusy(v){ busy=v; render(); $$('#recommendedBtn,#restoreBtn,#restartBtn,#restorePointBtn').forEach(x=>x.disabled=v); }
function esc(s=''){ return String(s).replace(/[&<>'"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c])); }
let toastTimer; function toast(msg,error=false){ const t=$('#toast'); t.textContent=msg;t.className='toast show'+(error?' error':'');clearTimeout(toastTimer);toastTimer=setTimeout(()=>t.className='toast',3600); }
function confirmBox(title,text,fn){ $('#modalTitle').textContent=title;$('#modalText').textContent=text;$('#modalWrap').hidden=false;$('#modalConfirm').onclick=()=>{$('#modalWrap').hidden=true;fn();}; }

function bind() {
  $('#search').oninput=e=>{query=e.target.value.trim().toLowerCase();render();};
  $$('.chip').forEach(b=>b.onclick=()=>{filter=b.dataset.filter;$$('.chip').forEach(x=>x.classList.toggle('active',x===b));render();});
  $('#modalCancel').onclick=()=>$('#modalWrap').hidden=true;
  $('#modalWrap').onclick=e=>{if(e.target===$('#modalWrap'))$('#modalWrap').hidden=true;};
  $('#recommendedBtn').onclick=()=>confirmBox('Рекомендуемый набор','Будут включены только безопасные рекомендуемые параметры. Текущие значения сохранятся для отката.',async()=>{setBusy(true);try{const r=await api('apply-recommended',{method:'POST',body:'{}'});toast(`Применено настроек: ${r.applied.length}`);await load();}catch(e){toast(e.message,true)}finally{setBusy(false)}});
  $('#restoreBtn').onclick=()=>confirmBox('Откатить изменения','Все параметры вернутся к значениям, сохранённым до первого изменения этой программой.',async()=>{setBusy(true);try{const r=await api('restore',{method:'POST',body:'{}'});toast(`Восстановлено значений: ${r.restored}`);await load();}catch(e){toast(e.message,true)}finally{setBusy(false)}});
  $('#restartBtn').onclick=()=>confirmBox('Перезапустить Проводник','Панель задач и окна Проводника исчезнут на несколько секунд.',async()=>{setBusy(true);try{await api('restart-explorer',{method:'POST',body:'{}'});toast('Проводник перезапущен');}catch(e){toast(e.message,true)}finally{setBusy(false)}});
  $('#restorePointBtn').onclick=async()=>{setBusy(true);try{await api('restore-point',{method:'POST',body:'{}'});toast('Точка восстановления создана');}catch(e){toast(e.message,true)}finally{setBusy(false)}};
  $('#exportBtn').onclick=async()=>{try{const r=await fetch('/api/export',{headers:{'X-Customizer-Token':TOKEN}});if(!r.ok)throw new Error('Не удалось экспортировать');const b=await r.blob(),a=document.createElement('a');a.href=URL.createObjectURL(b);a.download='win11-customizer-backup.json';a.click();URL.revokeObjectURL(a.href);}catch(e){toast(e.message,true)}};
}

initNav(); bind(); updateHeading(); updateClock(); setInterval(updateClock, 10000); load();


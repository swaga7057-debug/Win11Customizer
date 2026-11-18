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
  const headers = {'X-Customizer-Token':TOKEN,...(opts.headers||{})};
  if (!(opts.body instanceof FormData)) headers['Content-Type'] = 'application/json';
  const res = await fetch('/api/' + path, { ...opts, headers });
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
    renderTaskbarDrawer();
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

function renderTaskbarDrawer() {
  if (!state) return;
  const list = state.tweaks.filter(t => t.category === 'taskbar');
  $('#taskbarQuickList').innerHTML = list.map(t => `<div class="drawer-item">
    <span>▰</span><div><b>${esc(t.title)}</b><small>${esc(t.description)}</small></div>
    <label class="switch"><input type="checkbox" data-id="${t.id}" ${t.enabled?'checked':''} ${busy?'disabled':''}><span class="slider"></span></label>
  </div>`).join('');
  $$('#taskbarQuickList .switch input').forEach(input => input.onchange = () => requestToggle(input));
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
function setBusy(v){ busy=v; render(); renderTaskbarDrawer(); $$('#recommendedBtn,#restoreBtn,#restartBtn,#restorePointBtn,#chooseWallpaperBtn,#applyWallpaperBtn,#drawerRestartBtn,.online-wallpaper').forEach(x=>x.disabled=v);if(!v&&$('#applyWallpaperBtn'))$('#applyWallpaperBtn').disabled=!$('#wallpaperInput').files[0]; }
function esc(s=''){ return String(s).replace(/[&<>'"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c])); }
let toastTimer; function toast(msg,error=false){ const t=$('#toast'); t.textContent=msg;t.className='toast show'+(error?' error':'');clearTimeout(toastTimer);toastTimer=setTimeout(()=>t.className='toast',3600); }
function confirmBox(title,text,fn){ $('#modalTitle').textContent=title;$('#modalText').textContent=text;$('#modalWrap').hidden=false;$('#modalConfirm').onclick=()=>{$('#modalWrap').hidden=true;fn();}; }

function setTheme(name) {
  const theme = ['ocean','aurora','sunset','midnight','liquid'].includes(name) ? name : 'ocean';
  document.documentElement.dataset.theme = theme;
  localStorage.setItem('w11-theme', theme);
  $$('.theme-card').forEach(x => x.classList.toggle('active', x.dataset.theme === theme));
}

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
  $$('.theme-card').forEach(b => b.onclick=()=>setTheme(b.dataset.theme));
  const input=$('#wallpaperInput'), choose=$('#chooseWallpaperBtn'), apply=$('#applyWallpaperBtn'), preview=$('#wallpaperPreview');
  choose.onclick=()=>input.click();
  $('#wallpaperBox').ondragover=e=>{e.preventDefault();e.currentTarget.classList.add('drag')};
  $('#wallpaperBox').ondragleave=e=>e.currentTarget.classList.remove('drag');
  $('#wallpaperBox').ondrop=e=>{e.preventDefault();e.currentTarget.classList.remove('drag');if(e.dataTransfer.files[0]){input.files=e.dataTransfer.files;input.onchange();}};
  input.onchange=()=>{const file=input.files[0];if(!file)return;if(file.size>15*1024*1024){toast('Файл больше 15 МБ',true);input.value='';return;}const url=URL.createObjectURL(file);preview.style.backgroundImage=`url("${url}")`;preview.classList.add('has-image');apply.disabled=false;};
  apply.onclick=async()=>{const file=input.files[0];if(!file)return;const form=new FormData();form.append('wallpaper',file);setBusy(true);try{await api('wallpaper',{method:'POST',body:form});toast('Обои Windows установлены');await load();}catch(e){toast(e.message,true)}finally{setBusy(false);apply.disabled=!input.files[0];}};
  $$('.online-wallpaper').forEach(b=>b.onclick=()=>confirmBox('Установить обои',`Скачать стиль «${b.textContent.trim()}» из Unsplash и установить как фон Windows?`,async()=>{setBusy(true);try{await api('wallpaper/catalog',{method:'POST',body:JSON.stringify({id:b.dataset.wallpaper})});toast('Интернет-обои установлены');await load();}catch(e){toast(e.message,true)}finally{setBusy(false)}}));
  const drawer=$('#taskbarDrawer'), shade=$('#drawerShade');
  const openDrawer=()=>{drawer.classList.add('open');shade.classList.add('open');renderTaskbarDrawer()};
  const closeDrawer=()=>{drawer.classList.remove('open');shade.classList.remove('open')};
  $('#taskbarDrawerBtn').onclick=openDrawer;$('#closeTaskbarDrawer').onclick=closeDrawer;shade.onclick=closeDrawer;
  $('#drawerRestartBtn').onclick=()=>$('#restartBtn').click();
}

initNav(); bind(); setTheme(localStorage.getItem('w11-theme')||'ocean'); updateHeading(); updateClock(); setInterval(updateClock, 10000); load();


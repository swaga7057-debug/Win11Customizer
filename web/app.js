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
    const win = state.windows || {};
    $('#adminState').textContent = `${state.admin ? 'Администратор' : 'Обычный режим'}${win.build ? ` · ${win.displayVersion || 'Windows 11'} · ${win.build}` : ''}`;
    $('.status-dot').style.background = state.admin ? 'var(--green)' : '#f6bd55';
    $('#backupText').textContent = state.backupCount ? (state.backupOrderKnown===false ? `Старая копия: ${state.backupCount} значений · порядок неизвестен` : `Сохранено исходных значений: ${state.backupCount}`) : (state.themeBackup ? 'Сохранена прежняя тема Windows' : 'Резервная копия пуста');
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
  $('#grid').innerHTML = list.length ? list.map(t => `<article class="card ${t.available?'':'unavailable'}" title="${esc(t.error||t.description)}">
    <div class="card-icon">${cardIcons[t.category]||'•'}</div>
    <div class="card-body"><div class="card-title"><b>${esc(t.title)}</b>${t.recommended?'<span class="tag">СОВЕТ</span>':''}${t.experimental?'<span class="tag experimental">ЭКСПЕРИМЕНТ</span>':''}${t.risk==='medium'?'<span class="tag medium">ОСТОРОЖНО</span>':''}${t.admin?'<span class="tag admin">ADMIN</span>':''}${!t.available?'<span class="tag unavailable-tag">НЕ ПОДДЕРЖИВАЕТСЯ</span>':''}</div><p>${esc(t.available?t.description:(t.error||t.description))}</p></div>
    <label class="switch"><input type="checkbox" data-id="${t.id}" ${t.enabled?'checked':''} ${busy||!t.available?'disabled':''}><span class="slider"></span></label>
  </article>`).join('') : '<div class="empty">Настройки не найдены</div>';
  $$('.switch input').forEach(input => input.onchange = () => requestToggle(input));
}

function renderTaskbarDrawer() {
  if (!state) return;
  const list = state.tweaks.filter(t => t.category === 'taskbar');
  $('#taskbarQuickList').innerHTML = list.map(t => `<div class="drawer-item">
    <span>▰</span><div><b>${esc(t.title)}</b><small>${esc(t.available?t.description:(t.error||t.description))}</small></div>
    <label class="switch"><input type="checkbox" data-id="${t.id}" ${t.enabled?'checked':''} ${busy||!t.available?'disabled':''}><span class="slider"></span></label>
  </div>`).join('');
  $$('#taskbarQuickList .switch input').forEach(input => input.onchange = () => requestToggle(input));
}

async function requestToggle(input) {
  const t = state.tweaks.find(x=>x.id===input.dataset.id), enabled=input.checked;
  if (!t || !t.available) { if (t) toast(t.error || 'Настройка недоступна', true); return; }
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
function setBusy(v){ busy=v; render(); renderTaskbarDrawer(); $$('#recommendedBtn,#restoreBtn,#restartBtn,#restorePointBtn,#chooseWallpaperBtn,#applyWallpaperBtn,#drawerRestartBtn,#exportBtn,#profileExportBtn,#importProfileBtn,.online-wallpaper,.tool-card').forEach(x=>x.disabled=v);if(!v&&$('#applyWallpaperBtn'))$('#applyWallpaperBtn').disabled=!$('#wallpaperInput').files[0]; }
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
  $('#exportBtn').onclick=async()=>{try{const r=await fetch('/api/export',{headers:{'X-Customizer-Token':TOKEN}});if(!r.ok)throw new Error('Не удалось экспортировать резервную копию');const b=await r.blob(),a=document.createElement('a');a.href=URL.createObjectURL(b);a.download='win11-customizer-backup.json';a.click();URL.revokeObjectURL(a.href);toast('Резервная копия экспортирована');}catch(e){toast(e.message,true)}};
  $('#profileExportBtn').onclick=async()=>{try{const theme=document.documentElement.dataset.theme||'ocean';const r=await fetch(`/api/profile?theme=${encodeURIComponent(theme)}`,{headers:{'X-Customizer-Token':TOKEN}});if(!r.ok)throw new Error('Не удалось экспортировать профиль');const b=await r.blob(),a=document.createElement('a');a.href=URL.createObjectURL(b);a.download='win11-customizer-profile.json';a.click();URL.revokeObjectURL(a.href);toast('Профиль сохранён');}catch(e){toast(e.message,true)}};
  const profileInput=$('#profileInput');
  $('#importProfileBtn').onclick=()=>profileInput.click();
  profileInput.onchange=async()=>{const file=profileInput.files[0];if(!file)return;if(file.size>1024*1024){toast('Профиль больше 1 МБ',true);profileInput.value='';return;}try{const profile=JSON.parse(await file.text());const preview=await api('profile/preview',{method:'POST',body:JSON.stringify(profile)});const warning=preview.warnings?.length?` Экспериментальных параметров: ${preview.warnings.length}.`:'';const names=(preview.items||[]).slice(0,4).map(x=>`${x.enabled?'вкл.':'выкл.'} ${x.title}`).join(', '),more=preview.changes>4?` и ещё ${preview.changes-4}`:'';confirmBox('Применить профиль',`Изменений: ${preview.changes}${names?` — ${names}${more}`:''}.${warning} Перед применением сохранится точный снимок.`,async()=>{setBusy(true);try{const result=await api('profile/apply',{method:'POST',body:JSON.stringify(profile)});if(result.interfaceTheme)setTheme(result.interfaceTheme);toast(`Профиль применён · изменений: ${result.changed}`);await load();}catch(e){toast(e.message,true)}finally{setBusy(false);profileInput.value='';}});}catch(e){toast(e.message||'Некорректный профиль',true);profileInput.value='';}};
  $$('.theme-card').forEach(b => b.onclick=()=>setTheme(b.dataset.theme));
  const input=$('#wallpaperInput'), choose=$('#chooseWallpaperBtn'), apply=$('#applyWallpaperBtn'), preview=$('#wallpaperPreview');
  const slidesForm = $('#slidesForm'), slidesPanel = slidesForm.closest('details');
  async function slideCommand(action) {
    const options = action ? {method:'POST', body:JSON.stringify({
      action, folder:slidesForm.elements.folder.value.trim(), minutes:Number(slidesForm.elements.minutes.value)
    })} : {};
    try {
      const result = await api('slides', options);
      $('#slidesStatus').textContent = result.status;
      if (action) await load();
    } catch (error) { $('#slidesStatus').textContent = error.message; }
  }
  slidesForm.onsubmit = event => { event.preventDefault(); slideCommand('start'); };
  $('#slidesStop').onclick = () => slideCommand('stop');
  slidesPanel.ontoggle = () => { if (slidesPanel.open) slideCommand(); };
  setInterval(() => { if (slidesPanel.open && !document.hidden) slideCommand(); }, 10000);
  $('#onlinePhotoForm').onsubmit = event => {
    event.preventDefault();
    if (busy) return;
    const url = event.currentTarget.elements.photo.value.trim();
    confirmBox('Установить обои', 'Скачать выбранную фотографию с Unsplash и установить на рабочий стол?', async () => {
      setBusy(true);
      try {
        await api('wallpaper/catalog', {method:'POST', body:JSON.stringify({id:'custom', url})});
        toast('Фотография установлена');
        await load();
      } catch (error) { toast(error.message, true); }
      finally { setBusy(false); }
    });
  };
  choose.onclick=()=>input.click();
  $('#wallpaperBox').ondragover=e=>{e.preventDefault();e.currentTarget.classList.add('drag')};
  $('#wallpaperBox').ondragleave=e=>e.currentTarget.classList.remove('drag');
  $('#wallpaperBox').ondrop=e=>{e.preventDefault();e.currentTarget.classList.remove('drag');if(e.dataTransfer.files[0]){input.files=e.dataTransfer.files;input.onchange();}};
  const isThemeFile=file=>/\.(theme|themepack|deskthemepack)$/i.test(file.name);
  input.onchange=()=>{const file=input.files[0];if(!file)return;const themeFile=isThemeFile(file),limit=themeFile?50:15;if(file.size>limit*1024*1024){toast(`Файл больше ${limit} МБ`,true);input.value='';return;}if(themeFile){preview.style.backgroundImage='';preview.classList.remove('has-image');preview.querySelector('span').textContent='◈';}else{const url=URL.createObjectURL(file);preview.style.backgroundImage=`url("${url}")`;preview.classList.add('has-image');}apply.disabled=false;};
  apply.onclick=async()=>{const file=input.files[0];if(!file)return;const themeFile=isThemeFile(file),form=new FormData();form.append(themeFile?'theme':'wallpaper',file);setBusy(true);try{await api(themeFile?'theme':'wallpaper',{method:'POST',body:form});toast(themeFile?'Тема проверена и передана Windows':'Обои Windows установлены');await load();}catch(e){toast(e.message,true)}finally{setBusy(false);apply.disabled=!input.files[0];}};
  $$('.online-wallpaper').forEach(b=>b.onclick=()=>confirmBox('Установить обои',`Скачать стиль «${b.textContent.trim()}» из Unsplash и установить как фон Windows?`,async()=>{setBusy(true);try{await api('wallpaper/catalog',{method:'POST',body:JSON.stringify({id:b.dataset.wallpaper})});toast('Интернет-обои установлены');await load();}catch(e){toast(e.message,true)}finally{setBusy(false)}}));
  $$('.tool-card').forEach(b=>b.onclick=async()=>{if(b.dataset.action==='logs'){try{const r=await fetch('/api/logs',{headers:{'X-Customizer-Token':TOKEN}});if(!r.ok)throw new Error('Не удалось скачать журнал');const blob=await r.blob(),a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download='win11-customizer-audit.jsonl';a.click();URL.revokeObjectURL(a.href);toast('Журнал скачан');}catch(e){toast(e.message,true)}return;}setBusy(true);try{await api('open-settings',{method:'POST',body:JSON.stringify({id:b.dataset.settings})});toast(`Открыто: ${b.querySelector('b').textContent}`);}catch(e){toast(e.message,true)}finally{setBusy(false)}});
  const drawer=$('#taskbarDrawer'), shade=$('#drawerShade');
  const openDrawer=()=>{drawer.classList.add('open');shade.classList.add('open');renderTaskbarDrawer()};
  const closeDrawer=()=>{drawer.classList.remove('open');shade.classList.remove('open')};
  $('#taskbarDrawerBtn').onclick=openDrawer;$('#closeTaskbarDrawer').onclick=closeDrawer;shade.onclick=closeDrawer;
  $('#drawerRestartBtn').onclick=()=>$('#restartBtn').click();
}

initNav(); bind(); setTheme(localStorage.getItem('w11-theme')||'ocean'); updateHeading(); updateClock(); setInterval(updateClock, 10000); load();


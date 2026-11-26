function toggleTheme() {
  const next = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
  document.documentElement.setAttribute('data-theme', next);
  try { localStorage.setItem('theme', next); } catch (e) {}
  updateThemeButton();
}
function updateThemeButton() {
  const dark = document.documentElement.getAttribute('data-theme') === 'dark';
  const button = document.getElementById('theme-toggle');
  button.textContent = dark ? '☀️' : '🌙';
  button.setAttribute('aria-label', dark ? 'Switch to light theme' : 'Switch to dark theme');
}
updateThemeButton();
const $ = id => document.getElementById(id);
const err = e => { $('message').textContent = e.message || String(e); };
async function request(url, options) { const r = await fetch(url, options); if (!r.ok) throw new Error(await r.text()); return r.status === 202 ? null : r.json(); }
const flags = text => text.split('\n').map(s => s.trim()).filter(Boolean);
function selectHTML(choices, initial) { const e=document.createElement('select'); for (const [v,t] of choices) { const o=document.createElement('option');o.value=v;o.textContent=t;e.append(o); } e.value=initial;return e; }
function addStudent(path) {
  if (!path || [...document.querySelectorAll('.student')].some(x => x.dataset.path === path)) return;
  const row=document.createElement('div');row.className='student';row.dataset.path=path;
  const label=document.createElement('label'), check=document.createElement('input');check.type='checkbox';check.checked=true;label.append(check, document.createTextNode(' '+path));row.append(label);
  const details=document.createElement('details'), summary=document.createElement('summary');summary.textContent='Build overrides';details.append(summary);
  const std=selectHTML([['','Inherit standard'],...['c++11','c++14','c++17','c++20','c++23'].map(x=>[x,x])],'');std.className='std';
  const opt=selectHTML([['','Inherit optimization'],['default','Compiler default'],...['-O0','-O1','-O2','-O3','-Os'].map(x=>[x,x])],'');opt.className='opt';
  const extra=document.createElement('textarea');extra.placeholder='Additional arguments, one per line';extra.className='extra';
  const override=document.createElement('label'), cb=document.createElement('input');cb.type='checkbox';cb.className='override-extra';override.append(cb,document.createTextNode(' Override additional flags'));
  const effective=document.createElement('p');effective.className='muted effective';
  details.append(std,opt,override,extra,effective);row.append(details);$('students').append(row);updateEffective();
}
function buildFor(row) { const opt=row.querySelector('.opt').value;return { Standard:row.querySelector('.std').value, Optimization:opt, Extra:row.querySelector('.override-extra').checked?flags(row.querySelector('.extra').value):null }; }
function updateEffective(){for(const row of document.querySelectorAll('.student')){const b=buildFor(row), opt=row.querySelector('.opt').value;const args=['-std='+(b.Standard||$('standard').value)];if(opt==='default'){}else if(b.Optimization||$('optimization').value) args.push(b.Optimization||$('optimization').value);args.push(...(b.Extra===null?flags($('extra').value):b.Extra));row.querySelector('.effective').textContent='Effective flags: '+args.join(' ');}}
document.addEventListener('input',updateEffective);document.addEventListener('change',updateEffective);
$('add-student').onclick=()=>addStudent($('student-path').value.trim());
$('discover').onclick=async()=>{try{for(const p of await request('/api/discover?'+new URLSearchParams({base:$('base').value,pattern:$('pattern').value})))addStudent(p);}catch(e){err(e)}};
document.querySelectorAll('[data-browse]').forEach(button => {
  button.onclick = async () => {
    button.disabled = true;
    try {
      const response = await fetch('/api/pick-folder', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: $(button.dataset.browse).value })
      });
      if (!response.ok) throw new Error(await response.text());
      if (response.status !== 204) {
        const selected = await response.json();
        $(button.dataset.browse).value = selected.path;
        if (button.dataset.browse === 'tests') await loadExercises();
      }
    } catch (e) { err(e); }
    finally { button.disabled = false; }
  };
});
async function upload(entries,target){try{const form=new FormData();for(const [file,path] of entries){form.append('files',file,file.name);form.append('paths',path)}const roots=await request('/api/upload',{method:'POST',body:form});if(target==='tests'){if(roots.length!==1)throw new Error('Select one tests folder');$('tests').value=roots[0];await loadExercises()}else roots.forEach(addStudent)}catch(e){err(e)}}
for(const target of ['tests','students']) {$(target+'-upload').onchange=e=>upload([...e.target.files].map(f=>[f,f.webkitRelativePath]),target);const drop=$(target+'-drop');drop.ondragover=e=>{e.preventDefault();drop.classList.add('active')};drop.ondragleave=()=>drop.classList.remove('active');drop.ondrop=async e=>{e.preventDefault();drop.classList.remove('active');try{const out=[];async function walk(entry,prefix){if(entry.isFile){const f=await new Promise((res,rej)=>entry.file(res,rej));out.push([f,prefix+entry.name])}else if(entry.isDirectory){const reader=entry.createReader();let batch;do{batch=await new Promise((res,rej)=>reader.readEntries(res,rej));for(const child of batch)await walk(child,prefix+entry.name+'/')}while(batch.length)}}for(const item of e.dataTransfer.items){const entry=item.webkitGetAsEntry();if(entry)await walk(entry,'')}if(!out.length)throw new Error('No files found in dropped folders');await upload(out,target)}catch(ex){err(ex)}};}
let loadedTestsPath = '';
let exerciseRequest = 0;
$('tests').addEventListener('input', () => {
  loadedTestsPath = '';
  exerciseRequest++;
  $('selection').textContent = 'Choose or enter a tests folder to see its exercises.';
});
$('tests').addEventListener('change', loadExercises);
$('scan-tests').addEventListener('click', loadExercises);
async function loadExercises() {
  const path = $('tests').value.trim();
  const requestNumber = ++exerciseRequest;
  loadedTestsPath = '';
  if (!path) { $('selection').textContent = 'Choose a tests folder to see its exercises.'; return; }
  $('selection').textContent = 'Loading exercises…';
  try {
    const exercises = await request('/api/exercises?' + new URLSearchParams({path}));
    if (requestNumber !== exerciseRequest || path !== $('tests').value.trim()) return;
    const container = $('selection');
    container.replaceChildren();
    for (const exercise of exercises) {
      const row = document.createElement('div');
      row.className = 'exercise-selection';
      row.dataset.exercise = exercise.number;
      const label = document.createElement('label');
      const all = document.createElement('input');
      all.type = 'checkbox'; all.className = 'exercise-check';
      all.checked = exercise.cases.length > 0; all.disabled = !exercise.cases.length;
      label.append(all, document.createTextNode(` Exercise ${exercise.number} (${exercise.folder})`));
      row.append(label);
      if (!exercise.cases.length) {
        const note = document.createElement('span'); note.className = 'muted'; note.textContent = ' No input files found.'; row.append(note);
      } else {
        const details = document.createElement('details');
        const summary = document.createElement('summary'); summary.textContent = `${exercise.cases.length} input file(s)`; details.append(summary);
        for (const name of exercise.cases) {
          const caseLabel = document.createElement('label');
          const checkbox = document.createElement('input'); checkbox.type = 'checkbox'; checkbox.className = 'case-check'; checkbox.checked = true; checkbox.value = name;
          checkbox.onchange = () => {
            const cases = [...row.querySelectorAll('.case-check')];
            const count = cases.filter(item => item.checked).length;
            all.checked = count === cases.length; all.indeterminate = count > 0 && count < cases.length;
          };
          caseLabel.append(checkbox, document.createTextNode(' ' + name)); details.append(caseLabel);
        }
        all.onchange = () => { for (const checkbox of row.querySelectorAll('.case-check')) checkbox.checked = all.checked; all.indeterminate = false; };
        row.append(details);
      }
      container.append(row);
    }
    loadedTestsPath = path;
  } catch (e) {
    if (requestNumber !== exerciseRequest) return;
    $('selection').textContent = 'Could not load exercises.';
    err(e);
  }
}
async function runStudents(singleStudent) {
  try {
    $('message').textContent = '';
    if (!loadedTestsPath || loadedTestsPath !== $('tests').value.trim()) throw new Error('Choose a valid tests folder first.');
    const exercises = {};
    const rows = [...document.querySelectorAll('.exercise-selection')].filter(row => !row.querySelector('.exercise-check').disabled);
    let selectedCount = 0;
    let allSelected = rows.length > 0;
    for (const row of rows) {
      const cases = [...row.querySelectorAll('.case-check')];
      const selected = cases.filter(item => item.checked).map(item => item.value);
      if (selected.length > 0) {
        exercises[row.dataset.exercise] = selected.length === cases.length ? [] : selected;
        selectedCount += selected.length;
      }
      if (selected.length !== cases.length) allSelected = false;
    }
    if (!selectedCount) throw new Error('Select at least one exercise or input file.');
    const timeout = Number($('timeout').value);
    if (!Number.isInteger(timeout) || timeout < 1 || timeout > 86400) throw new Error('Timeout must be between 1 and 86400 seconds.');
    const students = [...document.querySelectorAll('.student')].filter(x => singleStudent ? x.dataset.path === singleStudent : x.querySelector('input[type=checkbox]').checked).map(x => ({Path:x.dataset.path,Build:buildFor(x)}));
    await request('/api/run',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({TestsFolder:loadedTestsPath,Students:students,Incremental:$('incremental').checked,Selection:{Exercises:allSelected ? {} : exercises},Timeout:timeout * 1000000000,Build:{Standard:$('standard').value,Optimization:$('optimization').value,Extra:flags($('extra').value)}})});
    refreshStatus();
  } catch (e) { if (singleStudent) $('show-setup').click(); err(e); }
}
$('run').onclick = () => runStudents();

let activeReport = '';
function showReport(url) {
  activeReport = url;
  $('report-frame').src = url;
  $('setup').hidden = true;
  $('report-panel').hidden = false;
  document.body.classList.add('report-open');
  document.querySelectorAll('.report-row').forEach(row => row.classList.toggle('active', row.querySelector('a').getAttribute('href') === url));
}
$('show-setup').onclick = () => { $('report-panel').hidden = true; $('setup').hidden = false; document.body.classList.remove('report-open'); document.querySelectorAll('.report-row.active').forEach(row => row.classList.remove('active')); };
$('show-summary').onclick = event => { event.preventDefault(); $('show-setup').click(); $('run-summary').scrollIntoView({behavior:'smooth'}); };
function handleReportClick(event) {
  if (event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
  event.preventDefault(); showReport(event.currentTarget.getAttribute('href'));
}
document.querySelectorAll('[data-report]').forEach(link => link.addEventListener('click', handleReportClick));
let resultSignature = '';
async function results() {
  const [data, files] = await Promise.all([request('/api/results'), request('/api/static-reports')]);
  const signature = JSON.stringify([data, files]);
  if (signature === resultSignature) return;
  const totals = await request('/api/summary');
  resultSignature = signature;
  renderSummary(totals);
  const container = $('results'); container.replaceChildren();
  const byName = new Set();
  for (const item of data || []) {
    const name = item.StudentName || item.ID;
    byName.add(name.trim().replaceAll(' ', '_'));
    const row = document.createElement('div');
    row.className = 'report-row';
    const link = document.createElement('a');
    link.href = '/reports/' + encodeURIComponent(name);
    link.textContent = name;
    link.addEventListener('click', handleReportClick);
    row.append(link);
    row.classList.toggle('active', !$('report-panel').hidden && link.getAttribute('href') === activeReport);
    if (item.StudentPath) {
      const button = document.createElement('button'); button.type = 'button'; button.className = 'rerun'; button.textContent = '🔄';
      button.title = `Rerun ${name}`; button.setAttribute('aria-label', button.title);
      button.onclick = () => { addStudent(item.StudentPath); runStudents(item.StudentPath); };
      row.append(button);
    }
    container.append(row);
  }
  for (const name of files) {
    if (byName.has(name)) continue;
    const row = document.createElement('div'); row.className = 'report-row';
    const link = document.createElement('a'); link.href = '/static/' + encodeURIComponent(name) + '/report.html';
    link.textContent = name; link.addEventListener('click', handleReportClick); row.append(link);
    row.classList.toggle('active', !$('report-panel').hidden && link.getAttribute('href') === activeReport); container.append(row);
  }
  if (!container.hasChildNodes()) container.textContent = 'No saved results yet.';
}
function appendRow(body, values) {
  const row = document.createElement('tr');
  for (const value of values) { const cell = document.createElement('td'); cell.textContent = String(value ?? 0); row.append(cell); }
  body.append(row);
}
function renderSummary(data) {
  const students = data.students || [];
  const rows = (data.summary && data.summary.Rows) || [];
  $('run-summary').hidden = students.length === 0;
  $('show-summary').hidden = students.length === 0;
  const studentTable = $('student-summary'); studentTable.replaceChildren();
  for (const student of students) {
    const missing = (student.Groups || []).reduce((n, group) => n + (group.Results || []).filter(item => item.FailureType === 'source_missing').length, 0);
    appendRow(studentTable, [student.StudentName, student.Total, student.Passed, student.Failed, student.CompileFails, student.RuntimeFails, student.DiffFails, missing]);
  }
  const exerciseTable = $('exercise-summary'); exerciseTable.replaceChildren();
  for (const row of rows) appendRow(exerciseTable, [row.Exercise, row.Total, row.AllPass, row.Partial, row.AllFail, row.CompileFail, row.NotSubmitted]);
}
let pollTimer;
let pollRunning = false;
function renderProgress(status) {
  const progress = status.progress || {};
  const total = progress.total_units || 0;
  const done = Math.min(progress.completed_units || 0, total);
  const students = progress.total_students || 0;
  const completedStudents = progress.completed_students || 0;
  const percent = total ? Math.round(done * 100 / total) : 0;
  $('progress-bar').max = total || 1;
  $('progress-bar').value = done;
  $('progress-percent').textContent = `${percent}%`;
  $('progress-label').textContent = status.running ? (total && done === total ? 'Finishing reports' : 'Running') : (status.error ? 'Run failed' : total ? 'Completed' : 'Ready');
  let counts = `${done} / ${total} steps · ${completedStudents} / ${students} students · ${progress.executed_cases || 0} run · ${progress.cached_cases || 0} cached`;
  if (status.running && status.started_at) {
    const elapsed = Math.max(0, Math.floor((Date.now() - Date.parse(status.started_at)) / 1000));
    if (Number.isFinite(elapsed)) counts += ` · ${elapsed}s`;
  }
  $('progress-counts').textContent = counts;
  $('progress-activity').textContent = progress.activity || (status.running ? 'Preparing run…' : 'No run in progress.');
  $('progress-log').textContent = (status.messages || []).join('\n');
}
async function refreshStatus() {
  if (pollRunning) return;
  clearTimeout(pollTimer);
  pollRunning = true;
  try {
    const status = await request('/api/status');
    renderProgress(status);
    $('run').disabled = status.running;
    if (status.error) $('message').textContent = status.error;
    await results();
  } catch (e) { err(e); }
  finally { pollRunning = false; pollTimer = setTimeout(refreshStatus, 1000); }
}
refreshStatus();

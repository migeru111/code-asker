(() => {
  'use strict';
  const view = document.getElementById('view');
  const titleEl = document.getElementById('title');
  const backBtn = document.getElementById('back');
  const tools = document.getElementById('tools');
  const wrapBtn = document.getElementById('wrap');
  const CHUNK = 500;

  const LANGS = {
    rs: 'rust', go: 'go', py: 'python', js: 'javascript', mjs: 'javascript', cjs: 'javascript', jsx: 'javascript',
    ts: 'typescript', tsx: 'typescript', c: 'c', h: 'c', cc: 'cpp', cpp: 'cpp', hpp: 'cpp', cxx: 'cpp',
    java: 'java', kt: 'kotlin', swift: 'swift', rb: 'ruby', php: 'php', lua: 'lua', pl: 'perl', sh: 'bash',
    bash: 'bash', zsh: 'bash', json: 'json', yaml: 'yaml', yml: 'yaml', toml: 'ini', ini: 'ini', md: 'markdown',
    html: 'xml', xml: 'xml', svg: 'xml', css: 'css', scss: 'scss', less: 'less', sql: 'sql', cs: 'csharp',
    diff: 'diff', patch: 'diff', mk: 'makefile', wat: 'wasm', r: 'r', graphql: 'graphql',
  };
  const NAMES = { makefile: 'makefile', dockerfile: 'bash', 'cargo.lock': 'ini', 'go.mod': 'go', 'go.sum': 'plaintext' };

  // ---- preferences (localStorage may be unavailable; fall back silently)
  const pref = {
    get(k, d) { try { const v = localStorage.getItem(k); return v === null ? d : v; } catch (e) { return d; } },
    set(k, v) { try { localStorage.setItem(k, v); } catch (e) { /* ignore */ } },
  };
  let fontSize = parseInt(pref.get('fs', '13'), 10) || 13;
  let wrap = pref.get('wrap', '0') === '1';
  function applyPrefs() {
    document.documentElement.style.setProperty('--fs', fontSize + 'px');
    document.body.classList.toggle('wrapon', wrap);
    wrapBtn.classList.toggle('on', wrap);
  }
  document.getElementById('fs-dec').onclick = () => { fontSize = Math.max(9, fontSize - 1); pref.set('fs', fontSize); applyPrefs(); };
  document.getElementById('fs-inc').onclick = () => { fontSize = Math.min(24, fontSize + 1); pref.set('fs', fontSize); applyPrefs(); };
  wrapBtn.onclick = () => { wrap = !wrap; pref.set('wrap', wrap ? '1' : '0'); applyPrefs(); };
  applyPrefs();

  // ---- helpers
  const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  const encPath = (p) => p.split('/').filter(Boolean).map(encodeURIComponent).join('/');
  class Unauthorized extends Error {}

  async function api(path, params) {
    const qs = new URLSearchParams(params || {}).toString();
    const res = await fetch('/api/' + path + (qs ? '?' + qs : ''), { credentials: 'same-origin' });
    if (res.status === 401) throw new Unauthorized();
    if (!res.ok) {
      let m = res.statusText;
      try { m = (await res.json()).error || m; } catch (e) { /* ignore */ }
      throw new Error(m);
    }
    return res.json();
  }

  function setHeader(title, back) {
    titleEl.innerHTML = '<bdi>' + esc(title) + '</bdi>';
    backBtn.hidden = !back;
    backBtn.onclick = () => { location.hash = back; };
  }

  function parseRoute() {
    const h = location.hash.replace(/^#/, '');
    const [p, q] = h.split('?');
    const seg = p.split('/').filter(Boolean).map(decodeURIComponent);
    const query = new URLSearchParams(q || '');
    if (seg[0] === 't' && seg[1]) return { kind: 'tree', repo: seg[1], path: seg.slice(2).join('/') };
    if (seg[0] === 'f' && seg[1] && seg[2]) return { kind: 'file', repo: seg[1], path: seg.slice(2).join('/'), line: parseInt(query.get('L'), 10) || 0 };
    return { kind: 'repos' };
  }

  const treeHash = (repo, path) => '#/t/' + encodeURIComponent(repo) + (path ? '/' + encPath(path) : '');
  const fileHash = (repo, path, line) => '#/f/' + encodeURIComponent(repo) + '/' + encPath(path) + (line ? '?L=' + line : '');
  const parentOf = (p) => p.split('/').slice(0, -1).join('/');

  // ---- views
  function showLogin(msg) {
    setHeader('code-asker', null);
    tools.hidden = true;
    view.innerHTML = '<form id="login"><h2>ログイン</h2><input id="tok" type="password" autocomplete="current-password" placeholder="トークン">' +
      '<button type="submit">開く</button><p class="err" id="lerr"></p></form>';
    document.getElementById('lerr').textContent = msg || '';
    document.getElementById('login').onsubmit = async (ev) => {
      ev.preventDefault();
      const token = document.getElementById('tok').value;
      const res = await fetch('/api/login', { method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }) });
      if (res.ok) { route(); return; }
      document.getElementById('lerr').textContent = res.status === 429 ? '試行回数が多すぎます。しばらく待ってください' : 'トークンが違います';
    };
  }

  function listHTML(items) {
    return '<ul class="list">' + items.map((i) =>
      '<li><a href="' + i.href + '"><span class="ico">' + i.icon + '</span><span class="name">' + esc(i.name) +
      '</span>' + (i.size ? '<span class="size">' + i.size + '</span>' : '') + '</a></li>').join('') + '</ul>';
  }
  const humanSize = (n) => n < 1024 ? n + ' B' : n < 1048576 ? (n / 1024).toFixed(1) + ' KB' : (n / 1048576).toFixed(1) + ' MB';

  async function showRepos() {
    setHeader('リポジトリ', null);
    tools.hidden = true;
    view.innerHTML = '<p class="msg">読み込み中…</p>';
    const repos = await api('repos');
    view.innerHTML = repos.length
      ? listHTML(repos.map((r) => ({ href: treeHash(r.name, ''), icon: '📦', name: r.name })))
      : '<p class="msg">リポジトリが見つかりません（CODE_ASKER_ROOT 直下の git リポジトリのみ表示します）</p>';
  }

  async function showTree(r) {
    setHeader(r.repo + (r.path ? '/' + r.path : ''), r.path ? treeHash(r.repo, parentOf(r.path)) : '#/');
    tools.hidden = true;
    view.innerHTML = '<p class="msg">読み込み中…</p>';
    const ents = await api('tree', { repo: r.repo, path: r.path });
    view.innerHTML = ents.length ? listHTML(ents.map((e) => {
      const p = (r.path ? r.path + '/' : '') + e.name;
      return e.type === 'dir'
        ? { href: treeHash(r.repo, p), icon: '📁', name: e.name }
        : { href: fileHash(r.repo, p, 0), icon: '📄', name: e.name, size: humanSize(e.size) };
    })) : '<p class="msg">空のディレクトリです</p>';
    window.scrollTo(0, 0);
  }

  // Highlight a block of text and split into per-line HTML with balanced spans.
  function highlightLines(lines, lang) {
    const text = lines.join('\n');
    let html;
    try {
      html = lang && window.hljs && hljs.getLanguage(lang)
        ? hljs.highlight(text, { language: lang, ignoreIllegals: true }).value : esc(text);
    } catch (e) { html = esc(text); }
    const out = []; const open = []; let cur = ''; let last = 0;
    const re = /<span[^>]*>|<\/span>|\n/g; let m;
    const flush = () => { out.push(cur + '</span>'.repeat(open.length)); cur = open.join(''); };
    while ((m = re.exec(html))) {
      cur += html.slice(last, m.index); last = re.lastIndex;
      if (m[0] === '\n') flush();
      else if (m[0] === '</span>') { open.pop(); cur += m[0]; } else { open.push(m[0]); cur += m[0]; }
    }
    cur += html.slice(last);
    out.push(cur + '</span>'.repeat(open.length));
    return out;
  }

  function langFor(path) {
    const base = path.split('/').pop().toLowerCase();
    if (NAMES[base]) return NAMES[base];
    const dot = base.lastIndexOf('.');
    return dot >= 0 ? LANGS[base.slice(dot + 1)] : null;
  }

  async function showFile(r) {
    const parent = treeHash(r.repo, parentOf(r.path));
    setHeader(r.repo + '/' + r.path, parent);
    tools.hidden = false;
    view.innerHTML = '<p class="msg">読み込み中…</p>';
    const lang = langFor(r.path);
    const first = r.line > 0 ? Math.max(1, Math.floor((r.line - 1) / CHUNK) * CHUNK + 1) : 1;
    const f = await api('file', { repo: r.repo, path: r.path, start: first, limit: CHUNK });
    if (f.binary || f.too_large) {
      view.innerHTML = '<p class="msg">' + (f.binary ? 'バイナリファイルは表示しません' : '大きすぎるため表示しません（' + humanSize(f.size) + '）') + '</p>';
      return;
    }
    const digits = String(f.total_lines).length;
    view.innerHTML = '<div id="code"></div>';
    const code = document.getElementById('code');
    const topBtn = mkMore('↑ 前の行を読み込む');
    const botBtn = mkMore('↓ 続きを読み込む');
    code.append(topBtn, botBtn);
    let lo = first, hi = first - 1;

    function rowsHTML(start, lines) {
      const hl = highlightLines(lines, lang);
      return hl.map((h, i) => {
        const n = start + i;
        return '<div class="row' + (n === r.line ? ' hl' : '') + '" data-n="' + n + '"><span class="ln" style="min-width:' + (digits + 1.5) + 'ch">' + n + '</span><span class="c hljs">' + (h || ' ') + '</span></div>';
      }).join('');
    }
    function update() { topBtn.hidden = lo <= 1; botBtn.hidden = hi >= f.total_lines; }
    function append(start, lines) { botBtn.insertAdjacentHTML('beforebegin', rowsHTML(start, lines)); hi = start + lines.length - 1; update(); }
    function prepend(start, lines) {
      const before = document.documentElement.scrollHeight;
      topBtn.insertAdjacentHTML('afterend', rowsHTML(start, lines));
      lo = start; update();
      window.scrollBy(0, document.documentElement.scrollHeight - before);
    }
    append(first, f.lines);
    topBtn.onclick = async () => {
      const s = Math.max(1, lo - CHUNK);
      const p = await api('file', { repo: r.repo, path: r.path, start: s, limit: lo - s });
      prepend(s, p.lines);
    };
    botBtn.onclick = loadNext;
    let loading = false;
    async function loadNext() {
      if (loading || hi >= f.total_lines) return;
      loading = true;
      try {
        const p = await api('file', { repo: r.repo, path: r.path, start: hi + 1, limit: CHUNK });
        append(hi + 1, p.lines);
      } finally { loading = false; }
    }
    if ('IntersectionObserver' in window) {
      new IntersectionObserver((es) => { if (es.some((e) => e.isIntersecting)) loadNext().catch(() => {}); },
        { rootMargin: '600px' }).observe(botBtn);
    }
    if (r.line > 0) {
      const el = code.querySelector('.row[data-n="' + r.line + '"]');
      if (el) el.scrollIntoView({ block: 'center' });
    } else window.scrollTo(0, 0);
  }

  function mkMore(label) {
    const b = document.createElement('button');
    b.type = 'button'; b.className = 'more'; b.textContent = label; b.hidden = true;
    return b;
  }

  // ---- router
  async function route() {
    const r = parseRoute();
    try {
      if (r.kind === 'file') await showFile(r);
      else if (r.kind === 'tree') await showTree(r);
      else await showRepos();
    } catch (e) {
      if (e instanceof Unauthorized) { showLogin(); return; }
      tools.hidden = true;
      view.innerHTML = '<p class="msg err"></p>';
      view.firstChild.textContent = 'エラー: ' + e.message;
    }
  }
  window.addEventListener('hashchange', route);
  route();
})();

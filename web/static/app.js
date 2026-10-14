(() => {
  'use strict';
  const view = document.getElementById('view');
  const titleEl = document.getElementById('title');
  const backBtn = document.getElementById('back');
  const tools = document.getElementById('tools');
  const wrapBtn = document.getElementById('wrap');
  const searchBtn = document.getElementById('search');
  const symBtn = document.getElementById('symbols');
  const sheet = document.getElementById('sheet');
  const chatPanel = document.getElementById('chat');
  const chatBtn = document.getElementById('chatbtn');
  const chatLog = document.getElementById('chat-log');
  const chatLoc = document.getElementById('chat-loc');
  const chatQ = document.getElementById('chat-q');
  const chatSend = document.getElementById('chat-send');
  const barEl = document.getElementById('bar');
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

  function closeSheet() { sheet.hidden = true; closeChat(); }
  document.getElementById('sheet-close').onclick = closeSheet;

  function setHeader(title, back, searchTarget) {
    fileApi = null; chatCtx = null;
    closeSheet();
    symBtn.hidden = true;
    searchBtn.hidden = !searchTarget;
    searchBtn.onclick = () => { location.hash = searchTarget; };
    titleEl.innerHTML = '<bdi>' + esc(title) + '</bdi>';
    backBtn.hidden = !back;
    backBtn.onclick = () => { location.hash = back; };
  }

  function parseRoute() {
    const h = location.hash.replace(/^#/, '');
    const [p, q] = h.split('?');
    const seg = p.split('/').filter(Boolean).map(decodeURIComponent);
    const query = new URLSearchParams(q || '');
    if (seg[0] === 's' && seg[1]) return { kind: 'search', repo: seg[1], path: seg.slice(2).join('/'), q: query.get('q') || '', regex: query.get('re') === '1' };
    if (seg[0] === 't' && seg[1]) return { kind: 'tree', repo: seg[1], path: seg.slice(2).join('/') };
    if (seg[0] === 'f' && seg[1] && seg[2]) return { kind: 'file', repo: seg[1], path: seg.slice(2).join('/'), line: parseInt(query.get('L'), 10) || 0 };
    return { kind: 'repos' };
  }

  const treeHash = (repo, path) => '#/t/' + encodeURIComponent(repo) + (path ? '/' + encPath(path) : '');
  const fileHash = (repo, path, line) => '#/f/' + encodeURIComponent(repo) + '/' + encPath(path) + (line ? '?L=' + line : '');
  const searchHash = (repo, path, q, regex) => '#/s/' + encodeURIComponent(repo) + (path ? '/' + encPath(path) : '') +
    (q ? '?q=' + encodeURIComponent(q) + (regex ? '&re=1' : '') : '');
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
    setHeader(r.repo + (r.path ? '/' + r.path : ''), r.path ? treeHash(r.repo, parentOf(r.path)) : '#/', searchHash(r.repo, r.path, '', false));
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
    setHeader(r.repo + '/' + r.path, parent, searchHash(r.repo, '', '', false));
    tools.hidden = false;
    symBtn.hidden = false;
    symBtn.onclick = null;
    chatCtx = { repo: r.repo, path: r.path };
    chatBtn.onclick = toggleChat;
    if (chat.repo !== r.repo) resetChat(r.repo);
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
    async function loadPrev() {
      const s = Math.max(1, lo - CHUNK);
      const p = await api('file', { repo: r.repo, path: r.path, start: s, limit: lo - s });
      prepend(s, p.lines);
    }
    topBtn.onclick = loadPrev;
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
    function highlightRow(n) {
      const prev = code.querySelector('.row.hl');
      if (prev) prev.classList.remove('hl');
      const el = code.querySelector('.row[data-n="' + n + '"]');
      if (!el) return false;
      el.classList.add('hl');
      el.scrollIntoView({ block: 'center' });
      return true;
    }
    function jump(n) {
      closeSheet();
      if (highlightRow(n)) {
        history.replaceState(null, '', fileHash(r.repo, r.path, n)); // no hashchange: stay on the page
      } else {
        location.hash = fileHash(r.repo, r.path, n); // not loaded yet: re-render around that line
      }
    }
    if (r.line > 0) highlightRow(r.line); else window.scrollTo(0, 0);

    let syms = null;
    async function getSyms() {
      if (!syms) syms = await api('symbols', { repo: r.repo, path: r.path });
      return syms;
    }

    // Used by the chat: bring a line range into view and mark it.
    const rowEl = (n) => code.querySelector('.row[data-n="' + n + '"]');
    const sleep = (ms) => new Promise((res) => setTimeout(res, ms));
    async function ensureLoaded(n) {
      for (let g = 0; g < 60 && n > hi && hi < f.total_lines; g++) { if (loading) await sleep(60); else await loadNext(); }
      for (let g = 0; g < 60 && n < lo && lo > 1; g++) await loadPrev();
    }
    async function reveal(start, end) {
      start = Math.max(1, Math.min(start, f.total_lines)); end = Math.max(start, Math.min(end, f.total_lines));
      await ensureLoaded(start); await ensureLoaded(end);
      code.querySelectorAll('.row.rng').forEach((e) => e.classList.remove('rng'));
      for (let n = start; n <= end; n++) { const e = rowEl(n); if (e) e.classList.add('rng'); }
      const el = rowEl(start);
      if (el) {
        window.scrollTo({ top: el.getBoundingClientRect().top + window.scrollY - barEl.offsetHeight - 8, behavior: 'smooth' });
        history.replaceState(null, '', fileHash(r.repo, r.path, start));
      }
    }
    fileApi = { symbols: getSyms, cachedSymbols: () => syms, reveal };

    symBtn.onclick = async () => {
      if (!sheet.hidden) { closeSheet(); return; }
      document.getElementById('sheet-title').textContent = 'シンボル';
      const body = document.getElementById('sheet-body');
      sheet.hidden = false;
      if (!syms) {
        body.innerHTML = '<p class="msg">読み込み中…</p>';
        try { await getSyms(); } catch (e) {
          body.innerHTML = '<p class="msg err"></p>'; body.firstChild.textContent = 'エラー: ' + e.message; return;
        }
      }
      if (!syms.length) { body.innerHTML = '<p class="msg">シンボルが見つかりません</p>'; return; }
      body.innerHTML = syms.map((s) => '<button type="button" class="item" data-line="' + s.line + '"><span class="kind">' + esc(s.kind) +
        '</span><span class="nm">' + (s.scope ? '<small>' + esc(s.scope) + '.</small>' : '') + esc(s.name) + '</span><span class="at">' + s.line + '</span></button>').join('');
      body.onclick = (ev) => { const b = ev.target.closest('.item'); if (b) jump(parseInt(b.dataset.line, 10)); };
    };
  }

  async function showSearch(r) {
    const back = r.path ? treeHash(r.repo, r.path) : treeHash(r.repo, '');
    setHeader('検索: ' + r.repo + (r.path ? '/' + r.path : ''), back, null);
    tools.hidden = true;
    view.innerHTML = '<form id="searchform"><input id="q" type="search" enterkeyhint="search" autocapitalize="off" autocorrect="off" spellcheck="false" placeholder="検索語">' +
      '<label><input type="checkbox" id="re"> 正規表現</label><button type="submit">検索</button></form><div id="results"></div>';
    const qEl = document.getElementById('q'); const reEl = document.getElementById('re'); const out = document.getElementById('results');
    qEl.value = r.q; reEl.checked = r.regex;
    async function run() {
      const q = qEl.value.trim();
      if (!q) return;
      history.replaceState(null, '', searchHash(r.repo, r.path, q, reEl.checked));
      out.innerHTML = '<p class="msg">検索中…</p>';
      let res;
      try { res = await api('search', { repo: r.repo, path: r.path, q, regex: reEl.checked ? '1' : '0' }); } catch (e) {
        if (e instanceof Unauthorized) { showLogin(); return; }
        out.innerHTML = '<p class="msg err"></p>'; out.firstChild.textContent = e.message; return;
      }
      if (!res.matches.length) { out.innerHTML = '<p class="msg">見つかりませんでした</p>'; return; }
      const needle = reEl.checked ? null : q.toLowerCase();
      out.innerHTML = res.matches.map((m) => {
        let t = m.text.trim(); if (t.length > 240) t = t.slice(0, 240) + '…';
        let h = esc(t);
        if (needle) { const i = t.toLowerCase().indexOf(needle); if (i >= 0) h = esc(t.slice(0, i)) + '<mark>' + esc(t.slice(i, i + needle.length)) + '</mark>' + esc(t.slice(i + needle.length)); }
        return '<a class="hit" href="' + fileHash(r.repo, m.path, m.line) + '"><div class="loc">' + esc(m.path) + ':' + m.line + '</div><div class="txt">' + h + '</div></a>';
      }).join('') + (res.truncated ? '<p class="msg">結果が多いため先頭 ' + res.matches.length + ' 件のみ表示しています</p>' : '');
    }
    document.getElementById('searchform').onsubmit = (ev) => { ev.preventDefault(); qEl.blur(); run(); };
    if (r.q) run();
  }

  function mkMore(label) {
    const b = document.createElement('button');
    b.type = 'button'; b.className = 'more'; b.textContent = label; b.hidden = true;
    return b;
  }

  // ---- AI chat
  const chat = { repo: null, session: '', busy: null };
  let chatCtx = null;
  let fileApi = null;
  const chatCands = document.getElementById('chat-cands');

  function resetChat(repo) {
    if (chat.busy) chat.busy.abort();
    chat.repo = repo; chat.session = ''; chat.busy = null;
    chatLog.innerHTML = '<p class="msg">コードを見ながら質問できます。見えている行範囲が自動で付きます。</p>';
  }
  function closeChat() { chatPanel.hidden = true; document.body.classList.remove('chat-open'); }
  function toggleChat() {
    if (!chatPanel.hidden) { closeChat(); return; }
    sheet.hidden = true;
    chatPanel.hidden = false;
    document.body.classList.add('chat-open');
    updateChatLoc();
    chatLog.scrollTop = chatLog.scrollHeight;
  }
  document.getElementById('chat-close').onclick = closeChat;
  document.getElementById('chat-new').onclick = () => { if (chatCtx) resetChat(chatCtx.repo); };

  // Lines currently visible above the chat panel, found by hit-testing rows (no scroll tracking needed).
  function visibleRange() {
    const x = Math.floor(window.innerWidth / 2);
    const top = barEl.getBoundingClientRect().bottom + 2;
    const bottom = (chatPanel.hidden ? window.innerHeight : chatPanel.getBoundingClientRect().top) - 2;
    const rowAt = (y, step) => {
      for (let i = 0; i < 12; i++, y += step) {
        const el = document.elementFromPoint(x, y);
        const row = el && el.closest && el.closest('.row');
        if (row) return parseInt(row.dataset.n, 10);
      }
      return 0;
    };
    const s = rowAt(top, 4); const e = rowAt(bottom, -4);
    return s && e ? { start: s, end: Math.max(s, e) } : { start: s || e || 1, end: e || s || 1 };
  }
  // Shows how the current question text will be interpreted (also used when sending).
  function updateChatLoc() {
    if (!chatCtx || chatPanel.hidden) return;
    const v = visibleRange();
    const loc = Locate.parseLocation(chatQ.value);
    let label = '📍 ' + chatCtx.path + '  L' + v.start + '–' + v.end + '（見えている範囲）';
    if (loc.kind === 'range') label = '📍 L' + loc.start + '–' + loc.end + '（行番号の指定）';
    else if (loc.kind === 'symbol') label = '🔎 「' + loc.name + '」を探します';
    else if (loc.kind === 'describe') label = '🔎 場所は AI が探します';
    else if (loc.kind === 'screen') {
      if (loc.which === 'function' || loc.which === 'type') {
        const syms = fileApi && fileApi.cachedSymbols();
        const sym = syms && Locate.enclosing(syms, Math.floor((v.start + v.end) / 2), loc.which === 'type' ? 'type' : 'func');
        label = sym ? '📍 ' + sym.name + '（L' + sym.line + '–' + (sym.end || '?') + '）' : '📍 画面の中央にある' + (loc.which === 'type' ? '型' : '関数');
      } else {
        const p = Locate.screenPart(v.start, v.end, loc.which);
        label = '📍 画面の' + { top: '上', bottom: '下', middle: '中央' }[loc.which] + ' L' + p.start + '–' + p.end;
      }
    }
    chatLoc.textContent = label;
    return v;
  }
  chatQ.addEventListener('input', () => requestAnimationFrame(updateChatLoc));

  let scrollTick = false;
  window.addEventListener('scroll', () => {
    if (chatPanel.hidden || scrollTick) return;
    scrollTick = true;
    requestAnimationFrame(() => { scrollTick = false; updateChatLoc(); });
  }, { passive: true });

  function linkify(html) {
    return html.replace(/([\w.\-\/]+\.\w+):(\d+)/g, (m, p, n) => chatCtx
      ? '<a href="' + fileHash(chatCtx.repo, p, parseInt(n, 10)) + '">' + m + '</a>' : m);
  }
  function inlineMd(t) {
    return t.split(/(`[^`\n]+`)/).map((part, i) => i % 2
      ? '<code>' + linkify(esc(part.slice(1, -1))) + '</code>'
      : linkify(esc(part).replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>')).replace(/\n/g, '<br>')).join('');
  }
  function renderMd(src) {
    return src.split(/(```[^\n]*\n[\s\S]*?(?:```|$))/).map((part, i) => {
      if (!(i % 2)) return inlineMd(part);
      const body = part.replace(/^```[^\n]*\n/, '').replace(/```$/, '');
      return '<pre><code>' + esc(body.replace(/\n$/, '')) + '</code></pre>';
    }).join('');
  }

  const clampEnd = (s, e) => Math.min(e, s + 299);
  function symbolTarget(c) {
    return { path: c.path, start: c.line, end: clampEnd(c.line, c.end || c.line + 59), reveal: c.path === chatCtx.path,
      label: c.name + '（' + c.kind + '）' };
  }

  // Turn the question into a concrete target: a line range, "let the AI search", or a list of candidates to confirm.
  async function resolveTarget(q) {
    const v = visibleRange(); const here = chatCtx.path;
    const loc = Locate.parseLocation(q);
    if (loc.kind === 'range') return { path: here, start: loc.start, end: loc.end, reveal: true, label: '行番号' };
    if (loc.kind === 'describe') return { search: true, path: here, label: '場所は AI が探す' };
    if (loc.kind === 'screen') {
      if (loc.which === 'function' || loc.which === 'type') {
        const syms = fileApi ? await fileApi.symbols().catch(() => []) : [];
        const sym = Locate.enclosing(syms, Math.floor((v.start + v.end) / 2), loc.which === 'type' ? 'type' : 'func');
        if (sym) return symbolTarget(Object.assign({ path: here }, sym));
        return { path: here, start: v.start, end: v.end, label: '見えている範囲' };
      }
      const p = Locate.screenPart(v.start, v.end, loc.which);
      return { path: here, start: p.start, end: p.end, label: '画面の' + { top: '上', bottom: '下', middle: '中央' }[loc.which] };
    }
    if (loc.kind === 'symbol') {
      const hits = await api('symsearch', { repo: chatCtx.repo, q: loc.name, path: here });
      const ranked = Locate.matchSymbols(loc.name, hits, loc.symClass)
        .sort((x, y) => y.score - x.score || (y.path === here) - (x.path === here)); // same score: current file first
      const want = Locate.norm(loc.name.split(/::|\./).pop());
      const exact = ranked.filter((c) => Locate.norm(c.name) === want);
      if (exact.length === 1) return symbolTarget(exact[0]);
      if (ranked.length) return { candidates: ranked.slice(0, 8), name: loc.name };
      return { search: true, path: here, label: '「' + loc.name + '」は見つからず AI が探す' };
    }
    return { path: here, start: v.start, end: v.end, label: '見えている範囲' };
  }

  function hideCands() { chatCands.hidden = true; chatCands.innerHTML = ''; }
  function showCands(question, list, name) {
    chatCands.innerHTML = '<div class="cands-head">「' + esc(name) + '」はどれですか？</div>';
    for (const c of list) {
      const b = document.createElement('button'); b.type = 'button'; b.className = 'cand';
      b.innerHTML = '<span class="kind">' + esc(c.kind) + '</span><span class="nm">' + (c.scope ? '<small>' + esc(c.scope) + '.</small>' : '') + esc(c.name) +
        '</span><span class="at">' + esc(c.path) + ':' + c.line + '</span>';
      b.onclick = () => send(question, symbolTarget(c));
      chatCands.append(b);
    }
    const ai = document.createElement('button'); ai.type = 'button'; ai.className = 'cand alt'; ai.textContent = '🔎 AI に探させる';
    ai.onclick = () => send(question, { search: true, path: chatCtx.path, label: '場所は AI が探す' });
    const cancel = document.createElement('button'); cancel.type = 'button'; cancel.className = 'cand alt'; cancel.textContent = 'キャンセル';
    cancel.onclick = hideCands;
    chatCands.append(ai, cancel);
    chatCands.hidden = false;
    chatCands.scrollIntoView({ block: 'nearest' });
  }

  document.getElementById('chat-form').onsubmit = async (ev) => {
    ev.preventDefault();
    if (chat.busy) { chat.busy.abort(); return; }
    const question = chatQ.value.trim();
    if (!question || !chatCtx) return;
    hideCands();
    chatSend.disabled = true;
    let t;
    try { t = await resolveTarget(question); } catch (e) {
      if (e instanceof Unauthorized) { showLogin(); return; }
      const v = visibleRange(); t = { path: chatCtx.path, start: v.start, end: v.end, label: '見えている範囲' };
    } finally { chatSend.disabled = false; }
    if (t.candidates) { showCands(question, t.candidates, t.name); return; }
    send(question, t);
  };

  async function send(question, t) {
    hideCands();
    chatQ.value = ''; chatQ.blur();
    if (chatLog.querySelector('p.msg')) chatLog.innerHTML = '';
    const where = t.search ? '🔎 ' + t.label : '📍 ' + t.path + ' L' + t.start + '–' + t.end + (t.label ? '（' + t.label + '）' : '');
    updateChatLoc();
    if (t.reveal && fileApi && t.path === chatCtx.path) fileApi.reveal(t.start, t.end).catch(() => {});
    const u = document.createElement('div'); u.className = 'msg-u';
    u.innerHTML = '<span class="at"></span>'; u.firstChild.textContent = where;
    u.append(question);
    const a = document.createElement('div'); a.className = 'msg-a'; a.textContent = '考え中…';
    chatLog.append(u, a); chatLog.scrollTop = chatLog.scrollHeight;

    const ctl = new AbortController(); chat.busy = ctl;
    chatSend.textContent = '停止'; chatSend.classList.add('stop');
    let text = ''; let tools = ''; let err = '';
    const paint = () => {
      a.innerHTML = tools + renderMd(text) + (err ? '<span class="err">' + esc(err) + '</span>' : '');
      chatLog.scrollTop = chatLog.scrollHeight;
    };
    try {
      const res = await fetch('/api/chat', { method: 'POST', credentials: 'same-origin', signal: ctl.signal,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ repo: chatCtx.repo, path: t.path, start: t.start || 0, end: t.end || 0, mode: t.search ? 'search' : '', question, session: chat.session }) });
      if (res.status === 401) { showLogin(); return; }
      if (!res.ok) { let m = res.statusText; try { m = (await res.json()).error || m; } catch (e) { /* ignore */ } throw new Error(m); }
      const reader = res.body.getReader(); const dec = new TextDecoder(); let buf = '';
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        let i;
        while ((i = buf.indexOf('\n\n')) >= 0) {
          const block = buf.slice(0, i); buf = buf.slice(i + 2);
          let ev2 = 'message'; let data = '';
          for (const line of block.split('\n')) {
            if (line.startsWith('event: ')) ev2 = line.slice(7);
            else if (line.startsWith('data: ')) data += line.slice(6);
          }
          if (!data) continue;
          const d = JSON.parse(data);
          if (ev2 === 'text') text += d.t;
          else if (ev2 === 'tool') tools += '<span class="tool">🔎 ' + esc(d.t) + '</span>';
          else if (ev2 === 'done') { if (d.session) chat.session = d.session; }
          else if (ev2 === 'error') err = d.t;
          paint();
        }
      }
      if (!text && !err) { text = '(回答が空でした)'; paint(); }
    } catch (e) {
      if (e.name === 'AbortError') { err = '(中断しました)'; } else { err = 'エラー: ' + e.message; }
      paint();
    } finally {
      chat.busy = null; chatSend.textContent = '送信'; chatSend.classList.remove('stop');
    }
  };

  // ---- router
  async function route() {
    const r = parseRoute();
    try {
      if (r.kind === 'file') await showFile(r);
      else if (r.kind === 'search') await showSearch(r);
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

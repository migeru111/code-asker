// Interprets where a spoken/typed question points to. Pure functions, no DOM.
// Loaded as a plain script in the browser (window.Locate) and via require() in tests.
(function (root) {
  'use strict';

  const KANJI = { '〇': 0, '零': 0, '一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9 };
  const UNITS = { '十': 10, '百': 100, '千': 1000 };

  // Full-width digits/letters -> ASCII, kanji numerals (二百五十 -> 250) -> digits.
  function normalize(text) {
    let s = text.replace(/[０-９Ａ-Ｚａ-ｚ＿]/g, (c) => String.fromCharCode(c.charCodeAt(0) - 0xFEE0));
    s = s.replace(/[〇零一二三四五六七八九十百千]+(?=\s*行)/g, (k) => String(kanjiToNumber(k)));
    return s.replace(/[　]/g, ' ');
  }

  function kanjiToNumber(k) {
    let total = 0; let cur = 0;
    for (const ch of k) {
      if (ch in KANJI) cur = cur * 10 + KANJI[ch];
      else if (ch in UNITS) { total += (cur || 1) * UNITS[ch]; cur = 0; }
    }
    return total + cur;
  }

  const FROM_LINES = 50;   // "120行目から" -> a screenful starting there
  const AROUND_BEFORE = 10, AROUND_AFTER = 30; // "120行目" -> a bit before and after

  const KIND_WORDS = '関数|メソッド|構造体|クラス|型|トレイト|trait|インターフェース|interface|列挙型|enum|struct|class|function|マクロ|定数|モジュール';
  const NAME = '[A-Za-z_][A-Za-z0-9_]*(?:(?:::|\\.)[A-Za-z_][A-Za-z0-9_]*)*';
  // "parse_header という関数" / "parse header という関数" / "parseHeader関数"
  const SYM_BEFORE = new RegExp('(' + NAME + '(?:\\s+[A-Za-z][A-Za-z0-9_]*){0,3})\\s*(?:という|っていう|と言う|と呼ばれる|の)?\\s*(' + KIND_WORDS + ')');
  // "関数 parse_header" / "関数「parse_header」"
  const SYM_AFTER = new RegExp('(' + KIND_WORDS + ')\\s*[「『"\'`]?(' + NAME + ')');
  const SYM_TICKS = /`([A-Za-z_][A-Za-z0-9_:.]*)`/;

  function kindClass(word) {
    if (/関数|メソッド|function|マクロ/.test(word)) return 'func';
    if (/構造体|クラス|型|トレイト|trait|インターフェース|interface|列挙型|enum|struct|class/.test(word)) return 'type';
    return 'any';
  }

  // Returns {kind:'range', start, end, focus} | {kind:'symbol', name, symClass}
  //   | {kind:'screen', which:'top'|'bottom'|'middle'|'function'|'type'} | {kind:'describe'} | {kind:'none'}
  function parseLocation(text) {
    const s = normalize(text || '');

    // 1. explicit line numbers: "120行目から150行目まで", "120〜150行", "120行目から", "120行目"
    let m = s.match(/(\d+)\s*行目?\s*(?:から|より)\s*(\d+)\s*行目?/) || s.match(/(\d+)\s*(?:から|〜|~|-|－|ー)\s*(\d+)\s*行/);
    if (m) {
      const a = parseInt(m[1], 10); const b = parseInt(m[2], 10);
      if (a >= 1 && b >= a) return { kind: 'range', start: a, end: b, focus: a };
    }
    m = s.match(/(\d+)\s*行目?\s*(から|より|以降|あたり|付近|辺り)?/);
    if (m) {
      const n = parseInt(m[1], 10);
      if (n >= 1) {
        return m[2] === 'から' || m[2] === 'より' || m[2] === '以降'
          ? { kind: 'range', start: n, end: n + FROM_LINES - 1, focus: n }
          : { kind: 'range', start: Math.max(1, n - AROUND_BEFORE), end: n + AROUND_AFTER, focus: n };
      }
    }

    // 2. symbol names
    m = s.match(SYM_BEFORE);
    if (m) return { kind: 'symbol', name: m[1].trim(), symClass: kindClass(m[2]) };
    m = s.match(SYM_AFTER);
    if (m) return { kind: 'symbol', name: m[2], symClass: kindClass(m[1]) };
    m = s.match(SYM_TICKS);
    if (m) return { kind: 'symbol', name: m[1], symClass: 'any' };

    // 3. relative to what is on screen
    if (/(画面|表示|見えて(?:る|いる)).{0,6}(上|うえ|先頭|冒頭)/.test(s) || /上のほう|上の方|上の部分|上あたり/.test(s)) return { kind: 'screen', which: 'top' };
    if (/(画面|表示|見えて(?:る|いる)).{0,6}(下|した|末尾)/.test(s) || /下のほう|下の方|下の部分|下あたり/.test(s)) return { kind: 'screen', which: 'bottom' };
    if (/真ん中|真中|中央|中ほど|中程|まんなか/.test(s)) return { kind: 'screen', which: 'middle' };
    if (/この(構造体|クラス|型|トレイト|インターフェース|列挙型)/.test(s)) return { kind: 'screen', which: 'type' };
    if (/この(関数|メソッド|処理|ブロック)|ここ(?:の|は|って|で)/.test(s)) return { kind: 'screen', which: 'function' };
    if (/(画面|見えて(?:る|いる)|表示).{0,4}(全体|全部|ところ|部分)/.test(s)) return { kind: 'none' };

    // 4. described in words -> the AI has to find it
    if (/(ところ|箇所|個所|部分|場所|とこ)(?:は|を|って|が|で|に|$|\s|[？?、。])/.test(s) && /(して(?:る|いる)|書き出|書いて|呼び出|読み込|使って|扱って|行って|定義|実装|処理)/.test(s)) return { kind: 'describe' };
    if (/どこ(?:で|に|が|にある|を)/.test(s)) return { kind: 'describe' };

    return { kind: 'none' };
  }

  const norm = (x) => String(x).toLowerCase().replace(/[^a-z0-9]/g, '');

  // Rank symbols by fuzzy name match ("parse header" == parse_header == parseHeader).
  // symClass 'func'|'type' gives a bonus (not a filter: ctags kind names vary per language).
  function matchSymbols(name, symbols, symClass) {
    const want = norm(name.split(/::|\./).pop()) || norm(name);
    if (want.length < 2) return [];
    const out = [];
    for (const sym of symbols) {
      const n = norm(sym.name);
      let score = 0;
      if (n === want) score = 3; else if (n.startsWith(want)) score = 2; else if (n.includes(want)) score = 1;
      if (!score) continue;
      if (symClass && symClass !== 'any' && kindOf(sym.kind) === symClass) score += 0.5;
      out.push(Object.assign({}, sym, { score }));
    }
    return out.sort((a, b) => b.score - a.score || a.line - b.line);
  }

  function kindOf(kind) {
    if (/func|method|member|constructor|macro|^fn$/i.test(kind)) return 'func';
    if (/struct|class|interface|type|enum|trait|implementation|union/i.test(kind)) return 'type';
    return 'other';
  }

  // The symbol enclosing line `line`: prefers one whose [line,end] contains it, else the nearest one above.
  function enclosing(symbols, line, cls) {
    let best = null;
    for (const sym of symbols) {
      if (cls && kindOf(sym.kind) !== cls) continue;
      if (sym.line > line) continue;
      const contains = !sym.end || sym.end >= line;
      if (contains && (!best || sym.line >= best.line)) best = sym;
    }
    if (best) return best;
    for (const sym of symbols) {
      if (cls && kindOf(sym.kind) !== cls) continue;
      if (sym.line <= line && (!best || sym.line > best.line)) best = sym;
    }
    return best;
  }

  // Part of the visible range [start,end]: 'top' | 'middle' | 'bottom' (a third each).
  function screenPart(start, end, which) {
    const span = end - start + 1;
    const third = Math.max(1, Math.ceil(span / 3));
    if (which === 'top') return { start, end: Math.min(end, start + third - 1) };
    if (which === 'bottom') return { start: Math.max(start, end - third + 1), end };
    const mid = start + Math.floor((span - third) / 2);
    return { start: mid, end: Math.min(end, mid + third - 1) };
  }

  const api = { normalize, parseLocation, matchSymbols, enclosing, screenPart, kindOf, norm, kanjiToNumber };
  if (typeof module !== 'undefined' && module.exports) module.exports = api; else root.Locate = api;
})(typeof window !== 'undefined' ? window : this);

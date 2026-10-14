'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const L = require('../static/locate.js');

const p = (t) => L.parseLocation(t);

test('line numbers', () => {
  assert.deepEqual(p('120行目から、この処理は何をしてる？'), { kind: 'range', start: 120, end: 169, focus: 120 });
  assert.deepEqual(p('120行目から150行目まで説明して'), { kind: 'range', start: 120, end: 150, focus: 120 });
  assert.deepEqual(p('120〜150行を要約して'), { kind: 'range', start: 120, end: 150, focus: 120 });
  assert.deepEqual(p('３０行目って何？'), { kind: 'range', start: 20, end: 60, focus: 30 }); // full-width digits
  assert.deepEqual(p('1行目は何'), { kind: 'range', start: 1, end: 31, focus: 1 });
  assert.equal(L.parseLocation('百二十行目から').start, 120); // kanji numerals
  assert.equal(L.parseLocation('二百五十行目').focus, 250);
});

test('symbol names', () => {
  assert.deepEqual(p('parse_header という関数は何をしてる？'), { kind: 'symbol', name: 'parse_header', symClass: 'func' });
  assert.deepEqual(p('parse header という関数を説明して'), { kind: 'symbol', name: 'parse header', symClass: 'func' });
  assert.deepEqual(p('ParseHeaderメソッドの引数は？'), { kind: 'symbol', name: 'ParseHeader', symClass: 'func' });
  assert.deepEqual(p('Store 構造体のフィールドは？'), { kind: 'symbol', name: 'Store', symClass: 'type' });
  assert.deepEqual(p('関数 parse_header の戻り値は'), { kind: 'symbol', name: 'parse_header', symClass: 'func' });
  assert.deepEqual(p('Header::parse という関数'), { kind: 'symbol', name: 'Header::parse', symClass: 'func' });
  assert.deepEqual(p('`resolve` の動きは'), { kind: 'symbol', name: 'resolve', symClass: 'any' });
});

test('relative to the screen', () => {
  assert.deepEqual(p('この関数は何をしてる？'), { kind: 'screen', which: 'function' });
  assert.deepEqual(p('この処理を説明して'), { kind: 'screen', which: 'function' });
  assert.deepEqual(p('この構造体の役割は？'), { kind: 'screen', which: 'type' });
  assert.deepEqual(p('画面の上のほうは何？'), { kind: 'screen', which: 'top' });
  assert.deepEqual(p('上のほうの処理は？'), { kind: 'screen', which: 'top' });
  assert.deepEqual(p('画面の下の方を見て'), { kind: 'screen', which: 'bottom' });
  assert.deepEqual(p('真ん中あたりは？'), { kind: 'screen', which: 'middle' });
});

test('descriptions are left to the AI', () => {
  assert.deepEqual(p('ログを書き出しているところはどこ？'), { kind: 'describe' });
  assert.deepEqual(p('エラーを握りつぶしている箇所を教えて'), { kind: 'describe' });
  assert.deepEqual(p('設定を読み込んでる部分は？'), { kind: 'describe' });
  assert.deepEqual(p('認証はどこで行われてる？'), { kind: 'describe' });
});

test('plain questions have no location', () => {
  assert.deepEqual(p('このリポジトリの概要は？'), { kind: 'none' });
  assert.deepEqual(p('Rust の所有権って何？'), { kind: 'none' });
  assert.deepEqual(p(''), { kind: 'none' });
});

test('explicit line number wins over relative words', () => {
  assert.equal(p('120行目から、この処理は何をしてる？').kind, 'range');
  assert.equal(p('parse_header という関数の120行目').kind, 'range');
});

const syms = [
  { name: 'Header', kind: 'struct', line: 1, end: 3 },
  { name: 'parse_header', kind: 'method', line: 10, end: 30, scope: 'Header' },
  { name: 'parse_headers_all', kind: 'function', line: 40, end: 60 },
  { name: 'ParseHeader', kind: 'function', line: 70, end: 90 },
  { name: 'unrelated', kind: 'function', line: 100, end: 110 },
];

test('fuzzy symbol matching', () => {
  const r = L.matchSymbols('parse header', syms, 'func');
  assert.deepEqual(r.map((x) => x.name), ['parse_header', 'ParseHeader', 'parse_headers_all']); // exact (x2, by line) before prefix
  assert.deepEqual(L.matchSymbols('parseHeader', syms).map((x) => x.name).slice(0, 2), ['parse_header', 'ParseHeader']);
  assert.deepEqual(L.matchSymbols('Header::parse', syms).length > 0, true); // last path segment
  assert.deepEqual(L.matchSymbols('x', syms), []);                           // too short to be meaningful
  assert.deepEqual(L.matchSymbols('nonexistent', syms), []);
});

test('enclosing symbol and screen parts', () => {
  assert.equal(L.enclosing(syms, 20, 'func').name, 'parse_header');
  assert.equal(L.enclosing(syms, 35, 'func').name, 'parse_header'); // between symbols: nearest above
  assert.equal(L.enclosing(syms, 2, 'type').name, 'Header');
  assert.equal(L.enclosing(syms, 5, 'func'), null);
  assert.deepEqual(L.screenPart(100, 148, 'top'), { start: 100, end: 116 });
  assert.deepEqual(L.screenPart(100, 148, 'bottom'), { start: 132, end: 148 });
  assert.deepEqual(L.screenPart(100, 148, 'middle'), { start: 116, end: 132 });
});

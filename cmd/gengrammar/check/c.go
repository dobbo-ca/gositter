package main

/*
#include <stdlib.h>
#include <stdio.h>
#include "parser.h"

// The C twin of mock.go.txt.
typedef struct {
  TSLexer base;
  const int32_t *in;
  uint32_t n, pos, mark, idle;
  uint64_t h;
} Mock;

#define MIX(m, v) ((m)->h = ((m)->h ^ (uint64_t)(v)) * 1099511628211ULL)

static void m_load(Mock *m) { m->base.lookahead = m->pos < m->n ? m->in[m->pos] : 0; }

static void m_advance(TSLexer *l, bool skip) {
  Mock *m = (Mock *)l;
  if (m->pos >= m->n) {
    if (++m->idle > (1u << 20)) { fprintf(stderr, "C lex function loops at end of input\n"); abort(); }
    return;
  }
  MIX(m, skip ? 2 : 1);
  MIX(m, m->pos);
  m->pos++;
  m_load(m);
}

static void m_mark_end(TSLexer *l) {
  Mock *m = (Mock *)l;
  MIX(m, 3);
  MIX(m, m->pos);
  m->mark = m->pos;
}

static uint32_t m_get_column(TSLexer *l) { return ((Mock *)l)->pos; }
static bool m_at_start(const TSLexer *l) { return ((const Mock *)l)->pos == 0; }
static bool m_eof(const TSLexer *l) { return ((const Mock *)l)->pos >= ((const Mock *)l)->n; }

static uint64_t run1(const TSLanguage *lang, int kw, uint16_t state, const int32_t *in, uint32_t n) {
  Mock m = {{0, 0, m_advance, m_mark_end, m_get_column, m_at_start, m_eof, NULL}, in, n, 0, 0, 0, 14695981039346656037ULL};
  m_load(&m);
  bool ret = (kw ? lang->keyword_lex_fn : lang->lex_fn)(&m.base, state);
  MIX(&m, m.base.result_symbol);
  MIX(&m, ret ? 1 : 0);
  MIX(&m, m.pos);
  MIX(&m, m.mark);
  return m.h;
}

// One run per start offset 0, step, 2*step, ... of in.
static void run_suffixes(const TSLanguage *lang, int kw, uint16_t state, const int32_t *in, uint32_t n, uint32_t step, uint64_t *out) {
  for (uint32_t p = 0; p <= n; p += step) *out++ = run1(lang, kw, state, in + p, n - p);
}

// One run per first code point; buf[1:n] is the shared tail.
static void run_firsts(const TSLanguage *lang, int kw, uint16_t state, int32_t *buf, uint32_t n, const int32_t *firsts, uint32_t nfirsts, uint64_t *out) {
  for (uint32_t i = 0; i < nfirsts; i++) {
    buf[0] = firsts[i];
    *out++ = run1(lang, kw, state, buf, n);
  }
}

static bool has_keyword_fn(const TSLanguage *lang) { return lang->keyword_lex_fn != NULL; }
static bool has_lex_fn(const TSLanguage *lang) { return lang->lex_fn != NULL; }
static bool has_create(const TSLanguage *lang) { return lang->external_scanner.create != NULL; }
*/
import "C"

import "unsafe"

type cLang = C.TSLanguage

func cRunSuffixes(l *cLang, kw bool, state uint16, in []int32, step int, out []uint64) {
	C.run_suffixes(l, cbool(kw), C.uint16_t(state), (*C.int32_t)(unsafe.SliceData(in)), C.uint32_t(len(in)), C.uint32_t(step), (*C.uint64_t)(unsafe.SliceData(out)))
}

func cRunFirsts(l *cLang, kw bool, state uint16, buf []int32, firsts []int32, out []uint64) {
	C.run_firsts(l, cbool(kw), C.uint16_t(state), (*C.int32_t)(unsafe.SliceData(buf)), C.uint32_t(len(buf)), (*C.int32_t)(unsafe.SliceData(firsts)), C.uint32_t(len(firsts)), (*C.uint64_t)(unsafe.SliceData(out)))
}

func cRun1(l *cLang, kw bool, state uint16, in []int32) uint64 {
	return uint64(C.run1(l, cbool(kw), C.uint16_t(state), (*C.int32_t)(unsafe.SliceData(in)), C.uint32_t(len(in))))
}

func cbool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

func cHasKeywordFn(l *cLang) bool { return bool(C.has_keyword_fn(l)) }
func cHasLexFn(l *cLang) bool     { return bool(C.has_lex_fn(l)) }
func cHasCreate(l *cLang) bool    { return bool(C.has_create(l)) }

// cSlice views n elements of a C array.
func cSlice[T any, P any](p *P, n int) []T {
	if p == nil {
		if n != 0 {
			panic("NULL table")
		}
		return nil
	}
	return unsafe.Slice((*T)(unsafe.Pointer(p)), n)
}

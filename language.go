package gositter

import (
	"cmp"
	"slices"
)

// language.c and language.h. The lookahead iterator, supertype and
// symbol_for_name functions serve queries only and are not ported.

// ts_language_symbol_count
func (l *Language) symbolCount() uint32 {
	return l.SymbolCount + l.AliasCount
}

// ts_language_table_entry. actions is a subslice of static data.
func (l *Language) tableEntry(state StateID, symbol Symbol) (actions []ParseAction, isReusable bool) {
	if symbol == SymbolError || symbol == SymbolErrorRepeat {
		return nil, false
	}
	actionIndex := uint32(l.lookup(state, symbol))
	entry := l.ParseActions[actionIndex]
	return l.ParseActions[actionIndex+1 : actionIndex+1+uint32(entry.Count())], entry.Reusable()
}

// ts_language_lex_mode_for_state
func (l *Language) lexModeForState(state StateID) LexMode {
	mode := l.LexModes[state]
	if l.ABIVersion < 15 {
		// TSLexMode has no reserved_word_set_id.
		mode.ReservedWordSetID = 0
	}
	return mode
}

// ts_language_is_reserved_word
func (l *Language) isReservedWord(state StateID, symbol Symbol) bool {
	lexMode := l.lexModeForState(state)
	if lexMode.ReservedWordSetID > 0 {
		start := uint32(lexMode.ReservedWordSetID) * uint32(l.MaxReservedWordSetSize)
		end := start + uint32(l.MaxReservedWordSetSize)
		for i := start; i < end; i++ {
			if l.ReservedWords[i] == symbol {
				return true
			}
			if l.ReservedWords[i] == 0 {
				break
			}
		}
	}
	return false
}

// ts_language_symbol_metadata
func (l *Language) symbolMetadata(symbol Symbol) SymbolMetadata {
	if symbol == SymbolError {
		return SymbolMetadata{Visible: true, Named: true}
	} else if symbol == SymbolErrorRepeat {
		return SymbolMetadata{}
	}
	return l.SymbolMetadata[symbol]
}

// ts_language_public_symbol
func (l *Language) publicSymbol(symbol Symbol) Symbol {
	if symbol == SymbolError {
		return symbol
	}
	return l.PublicSymbolMap[symbol]
}

// ts_language_next_state
func (l *Language) nextState(state StateID, symbol Symbol) StateID {
	if symbol == SymbolError || symbol == SymbolErrorRepeat {
		return 0
	} else if uint32(symbol) < l.TokenCount {
		actions, _ := l.tableEntry(state, symbol)
		if count := len(actions); count > 0 {
			action := actions[count-1]
			if action.Type == ParseActionTypeShift {
				if action.Extra() {
					return state
				}
				return action.State()
			}
		}
		return 0
	}
	return StateID(l.lookup(state, symbol))
}

// ts_language_symbol_name
func (l *Language) SymbolName(s Symbol) string {
	if s == SymbolError {
		return "ERROR"
	} else if s == SymbolErrorRepeat {
		return "_ERROR"
	} else if uint32(s) < l.symbolCount() {
		return l.SymbolNames[s]
	}
	return ""
}

// ts_language_field_name_for_id. Returns "" for 0 or out of range.
func (l *Language) FieldNameForID(id FieldID) string {
	count := l.FieldCount
	if count != 0 && uint32(id) <= count {
		return l.FieldNames[id]
	}
	return ""
}

// ts_language_field_id_for_name. Returns 0 if there is no such field.
func (l *Language) FieldIDForName(name string) FieldID {
	count := uint32(uint16(l.FieldCount))
	for i := uint32(1); i < count+1; i++ {
		field := l.FieldNames[i]
		// strncmp(name, field, len(name)). C switches on the result with
		// "case -1", and darwin's strncmp returns the byte difference, so
		// only a difference of exactly -1 ends the scan early.
		cmp := 0
		for j := 0; j < len(name); j++ {
			var fc byte
			if j < len(field) {
				fc = field[j]
			}
			if name[j] != fc {
				cmp = int(name[j]) - int(fc)
				break
			}
			if fc == 0 {
				break
			}
		}
		switch cmp {
		case 0:
			if len(field) <= len(name) {
				return FieldID(i)
			}
		case -1:
			return 0
		}
	}
	return 0
}

// ts_language_has_reduce_action
func (l *Language) hasReduceAction(state StateID, symbol Symbol) bool {
	actions, _ := l.tableEntry(state, symbol)
	return len(actions) > 0 && actions[0].Type == ParseActionTypeReduce
}

// ts_language_lookup. For non-terminals the value is a successor state, for
// terminals an index into ParseActions.
func (l *Language) lookup(state StateID, symbol Symbol) uint16 {
	if uint32(state) >= l.LargeStateCount {
		return l.lookupSmall(state, symbol)
	}
	return l.ParseTable[uint32(state)*l.SymbolCount+uint32(symbol)]
}

// The small-state half of ts_language_lookup. smallComb holds what the scan
// returns: two loads instead of a search.
func (l *Language) lookupSmall(state StateID, symbol Symbol) uint16 {
	st := uint32(state) - l.LargeStateCount
	if st < uint32(len(l.smallBase)) && uint32(symbol) < l.SymbolCount {
		// A slot belongs to the state whose number it carries; any other
		// slot means the scan would find nothing.
		if e := l.smallComb[l.smallBase[st]+uint32(symbol)]; e>>16 == st+1 {
			return uint16(e)
		}
		return 0
	}
	return l.scanSmall(state, symbol)
}

// The small parse table as C scans it.
func (l *Language) scanSmall(state StateID, symbol Symbol) uint16 {
	index := l.SmallParseTableMap[uint32(state)-l.LargeStateCount]
	data := l.SmallParseTable[index:]
	groupCount := data[0]
	pos := 1
	for i := uint16(0); i < groupCount; i++ {
		sectionValue := data[pos]
		symbolCount := int(data[pos+1])
		pos += 2
		for _, s := range data[pos : pos+symbolCount] {
			if s == uint16(symbol) {
				return sectionValue
			}
		}
		pos += symbolCount
	}
	return 0
}

const combLongRow = 4 // entries; see prepare

// prepare builds the derived lookup table once per Language. Parsers call it
// from SetLanguage, so the table is published before any parser reads it.
//
// The table is the small parse table as a row-displacement ("comb") table:
// the row of small state st is laid over one shared array at offset
// smallBase[st], wherever its non-zero entries hit only free slots. A slot is
// (st+1)<<16 | value, and 0 when free. It takes about 4 bytes per entry of
// the small parse table, where one dense row per state took up to 16 MB
// (scala) and was not built at all for verilog.
func (l *Language) prepare() {
	l.prepareOnce.Do(func() {
		if l.StateCount <= l.LargeStateCount {
			return
		}
		small := l.StateCount - l.LargeStateCount
		// The state number in a slot has 16 bits.
		if small >= 0xFFFF || uint32(len(l.SmallParseTableMap)) < small {
			return
		}

		// The rows: for each state the (symbol, value) pairs the scan can
		// return. The scan returns the first match, and a zero value is the
		// same as no entry.
		type entry struct{ symbol, value uint16 }
		entries := make([]entry, 0, len(l.SmallParseTable))
		rowStart := make([]uint32, small+1)
		seen := make([]uint32, l.SymbolCount)
		for st := uint32(0); st < small; st++ {
			rowStart[st] = uint32(len(entries))
			data := l.SmallParseTable[l.SmallParseTableMap[st]:]
			groupCount := data[0]
			pos := 1
			for i := uint16(0); i < groupCount; i++ {
				sectionValue := data[pos]
				symbolCount := int(data[pos+1])
				pos += 2
				for _, s := range data[pos : pos+symbolCount] {
					if uint32(s) < l.SymbolCount && seen[s] != st+1 {
						seen[s] = st + 1
						if sectionValue != 0 {
							entries = append(entries, entry{s, sectionValue})
						}
					}
				}
				pos += symbolCount
			}
		}
		rowStart[small] = uint32(len(entries))

		// First fit, longest rows first: they are the hardest to place.
		order := make([]uint32, small)
		for i := range order {
			order[i] = uint32(i)
		}
		rowLen := func(st uint32) uint32 { return rowStart[st+1] - rowStart[st] }
		slices.SortStableFunc(order, func(a, b uint32) int { return cmp.Compare(rowLen(b), rowLen(a)) })

		base := make([]uint32, small)
		comb := make([]uint32, l.SymbolCount, uint32(len(entries))+l.SymbolCount)
		lowest := uint32(0) // no free slot below this index
		longBase := uint32(0)
		for _, st := range order {
			row := entries[rowStart[st]:rowStart[st+1]]
			if len(row) == 0 {
				continue
			}
			for lowest < uint32(len(comb)) && comb[lowest] != 0 {
				lowest++
			}
			minSymbol := uint32(row[0].symbol)
			for _, e := range row {
				minSymbol = min(minSymbol, uint32(e.symbol))
			}
			// Short rows search from the lowest free slot and fill holes.
			// Long rows overlap each other too much for that: each would
			// walk the whole filled region, 40 s in total for scala. They
			// start where the last long row went, and that cursor only
			// moves forward.
			b := uint32(0)
			if lowest > minSymbol {
				b = lowest - minSymbol
			}
			long := len(row) >= combLongRow
			if long && longBase > b {
				b = longBase
			}
		fit:
			for ; ; b++ {
				for _, e := range row {
					if i := b + uint32(e.symbol); i < uint32(len(comb)) && comb[i] != 0 {
						continue fit
					}
				}
				break
			}
			if long {
				longBase = b
			}
			if need := int(b + l.SymbolCount); need > len(comb) {
				comb = append(comb, make([]uint32, need-len(comb))...)
			}
			for _, e := range row {
				comb[b+uint32(e.symbol)] = (st+1)<<16 | uint32(e.value)
			}
			base[st] = b
		}
		l.smallComb = comb
		l.smallBase = base
	})
}

// ts_language_has_actions
func (l *Language) hasActions(state StateID, symbol Symbol) bool {
	return l.lookup(state, symbol) != 0
}

// ts_language_enabled_external_tokens
func (l *Language) enabledExternalTokens(externalScannerState uint32) []bool {
	if externalScannerState == 0 {
		return nil
	}
	n := l.ExternalTokenCount
	return l.ExternalScanner.States[n*externalScannerState : n*(externalScannerState+1)]
}

// ts_language_alias_sequence. nil where C returns NULL. The slice runs to the
// end of the table because C indexes it without a bound.
func (l *Language) aliasSequence(productionID uint32) []Symbol {
	if productionID != 0 {
		return l.AliasSequences[productionID*uint32(l.MaxAliasSequenceLength):]
	}
	return nil
}

// ts_language_alias_at
func (l *Language) aliasAt(productionID, childIndex uint32) Symbol {
	if productionID != 0 {
		return l.AliasSequences[productionID*uint32(l.MaxAliasSequenceLength)+childIndex]
	}
	return 0
}

// ts_language_field_map
func (l *Language) fieldMap(productionID uint32) []FieldMapEntry {
	if l.FieldCount == 0 {
		return nil
	}
	slice := l.FieldMapSlices[productionID]
	return l.FieldMapEntries[slice.Index : uint32(slice.Index)+uint32(slice.Length)]
}

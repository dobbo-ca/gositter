package gositter

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

// The small-state half of ts_language_lookup. smallDense holds what the scan
// returns: one load instead of a search.
func (l *Language) lookupSmall(state StateID, symbol Symbol) uint16 {
	if uint32(symbol) < l.smallDenseSymbols {
		return l.smallDense[(uint32(state)-l.LargeStateCount)*l.smallDenseSymbols+uint32(symbol)]
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

// ponytail: 2 bytes per small state and symbol. Above this many entries
// (verilog: 16.4M) the table is not built and lookup scans as C does. A
// row-displacement table from the generator would cover those too.
const maxSmallDenseEntries = 12 << 20

// prepare builds the derived lookup tables once per Language. Parsers call it
// from SetLanguage, so the tables are published before any parser reads them.
func (l *Language) prepare() {
	l.prepareOnce.Do(func() {
		if l.StateCount <= l.LargeStateCount {
			return
		}
		small := l.StateCount - l.LargeStateCount
		n := uint64(small) * uint64(l.SymbolCount)
		if n > maxSmallDenseEntries || uint32(len(l.SmallParseTableMap)) < small {
			return
		}
		dense := make([]uint16, n)
		seen := make([]uint32, l.SymbolCount) // the scan returns the first match
		for st := uint32(0); st < small; st++ {
			row := dense[st*l.SymbolCount : (st+1)*l.SymbolCount]
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
						row[s] = sectionValue
					}
				}
				pos += symbolCount
			}
		}
		l.smallDense = dense
		l.smallDenseSymbols = l.SymbolCount
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

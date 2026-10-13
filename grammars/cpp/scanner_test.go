package cpp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dobbo-ca/gositter"
)

func serialize(s *scanner) []byte {
	buf := make([]byte, gositter.SerializationBufferSize)
	return buf[:s.Serialize(buf)]
}

func TestSerializeBytes(t *testing.T) {
	s := newScanner().(*scanner)
	if b := serialize(s); len(b) != 0 {
		t.Fatalf("fresh scanner = %v, want empty", b)
	}

	// 'a', U+00E9, U+1F600, a real NUL and TS_DECODE_ERROR (-1), as 4-byte
	// little-endian wchar_t.
	s.delimiterLength = 5
	copy(s.delimiter[:], []int32{'a', 0xE9, 0x1F600, 0, -1})
	want := []byte{
		'a', 0, 0, 0,
		0xE9, 0, 0, 0,
		0x00, 0xF6, 0x01, 0,
		0, 0, 0, 0,
		0xFF, 0xFF, 0xFF, 0xFF,
	}
	b := serialize(s)
	if !bytes.Equal(b, want) {
		t.Fatalf("bytes = %v, want %v", b, want)
	}

	// Dirty target: only the first delimiterLength entries count.
	got := &scanner{delimiterLength: 16}
	for i := range got.delimiter {
		got.delimiter[i] = 'z'
	}
	got.Deserialize(b)
	if got.delimiterLength != 5 || got.delimiter[4] != -1 || got.delimiter[2] != 0x1F600 {
		t.Fatalf("state = %+v", got)
	}
	if b2 := serialize(got); !bytes.Equal(b, b2) {
		t.Fatalf("serialize after deserialize differs:\n%v\n%v", b, b2)
	}
}

func TestDeserializeEmpty(t *testing.T) {
	s := &scanner{delimiterLength: 2}
	s.delimiter[0], s.delimiter[1] = 'x', 'y'
	s.Deserialize(nil)
	if s.delimiterLength != 0 || len(serialize(s)) != 0 {
		t.Fatalf("state = %+v", s)
	}
	// C does not memcpy when length is 0, so the old chars stay.
	if s.delimiter[0] != 'x' {
		t.Fatal("delimiter was cleared")
	}
}

func TestSerializeMax(t *testing.T) {
	s := newScanner().(*scanner)
	s.delimiterLength = maxDelimiterLength
	for i := range s.delimiter {
		s.delimiter[i] = int32('A' + i)
	}
	b := serialize(s)
	if len(b) != 64 {
		t.Fatalf("len = %d, want 64", len(b))
	}
	got := newScanner().(*scanner)
	got.Deserialize(b)
	if *got != *s {
		t.Fatalf("state = %+v, want %+v", got, s)
	}
}

// Raw strings through the parser: the scanner is wired in and its tokens
// land where the C grammar puts them.
func TestRawStrings(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`auto s = R"x(a)y)x";`, `(raw_string_literal delimiter: (raw_string_delimiter) (raw_string_content) (raw_string_delimiter))`},
		{`auto s = R"(a"b)";`, `(raw_string_literal (raw_string_content))`},
		// 17 chars: over MAX_DELIMITER_LENGTH, no delimiter token.
		{`auto s = R"abcdefghijklmnopq(a)abcdefghijklmnopq";`, `ERROR`},
		// Unterminated: content runs to EOF.
		{`auto s = R"x(abc`, `(raw_string_content)`},
	} {
		p := gositter.NewParser()
		p.SetLanguage(Language())
		tree := p.Parse([]byte(tc.src))
		if got := tree.RootNode().String(); !strings.Contains(got, tc.want) {
			t.Errorf("%s\n got %s\nwant %s", tc.src, got, tc.want)
		}
	}
}

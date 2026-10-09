package hcl

import (
	"bytes"
	"testing"

	"github.com/dobbo-ca/gositter"
)

func stack(ctxs ...context) *scanner { return &scanner{contextStack: ctxs} }

func serialize(s *scanner) []byte {
	buf := make([]byte, gositter.SerializationBufferSize)
	return buf[:s.Serialize(buf)]
}

func TestSerializeBytes(t *testing.T) {
	if b := serialize(newScanner().(*scanner)); !bytes.Equal(b, []byte{0, 0, 0, 0}) {
		t.Fatalf("fresh scanner = %v, want four zero bytes", b)
	}

	s := stack(
		context{typ: ctxHeredocTemplate, heredocIdentifier: []byte("EOT")},
		context{typ: ctxTemplateInterpolation},
		context{typ: ctxQuotedTemplate},
		context{typ: ctxTemplateDirective},
	)
	// u32 count, then per context: u32 type, u32 length, identifier bytes.
	want := []byte{
		4, 0, 0, 0,
		3, 0, 0, 0, 3, 0, 0, 0, 'E', 'O', 'T',
		0, 0, 0, 0, 0, 0, 0, 0,
		2, 0, 0, 0, 0, 0, 0, 0,
		1, 0, 0, 0, 0, 0, 0, 0,
	}
	b := serialize(s)
	if !bytes.Equal(b, want) {
		t.Fatalf("bytes = %v, want %v", b, want)
	}

	// Dirty target with a longer stale identifier: all of it must go.
	got := stack(context{typ: ctxQuotedTemplate, heredocIdentifier: []byte("STALESTALE")}, context{typ: 9})
	got.Deserialize(b)
	if b2 := serialize(got); !bytes.Equal(b, b2) {
		t.Fatalf("serialize after deserialize differs:\n%v\n%v", b, b2)
	}
	if len(got.contextStack) != 4 || string(got.contextStack[0].heredocIdentifier) != "EOT" ||
		got.contextStack[3].typ != ctxTemplateDirective {
		t.Fatalf("state = %+v", got.contextStack)
	}

	// The buffer belongs to the runtime; the scanner must hold a copy.
	b[12] = 'X'
	if string(got.contextStack[0].heredocIdentifier) != "EOT" {
		t.Fatal("Deserialize kept a reference to the buffer")
	}
}

func TestDeserializeEmpty(t *testing.T) {
	s := stack(context{typ: ctxHeredocTemplate, heredocIdentifier: []byte("EOT")})
	s.Deserialize(nil)
	if len(s.contextStack) != 0 {
		t.Fatalf("state = %+v", s.contextStack)
	}
	// A reused slot must not show the old identifier.
	if ctx := s.push(ctxQuotedTemplate); len(ctx.heredocIdentifier) != 0 {
		t.Fatalf("reused identifier = %q", ctx.heredocIdentifier)
	}
}

func TestSerializeLimits(t *testing.T) {
	n := func(count int, id string) int {
		s := &scanner{}
		for i := 0; i < count; i++ {
			s.push(ctxHeredocTemplate).heredocIdentifier = []byte(id)
		}
		return len(serialize(s))
	}
	// More than CHAR_MAX contexts, or an identifier over CHAR_MAX: C writes 0.
	if got := n(127, ""); got != 4+127*8 {
		t.Fatalf("127 contexts = %d", got)
	}
	if got := n(128, ""); got != 0 {
		t.Fatalf("128 contexts = %d, want 0", got)
	}
	id127 := string(bytes.Repeat([]byte{'a'}, 127))
	if got := n(1, id127); got != 4+8+127 {
		t.Fatalf("identifier of 127 = %d", got)
	}
	want128 := 0
	if charMax == 255 { // unsigned char: 128 fits
		want128 = 4 + 8 + 128
	}
	if got := n(1, id127+"a"); got != want128 {
		t.Fatalf("identifier of 128 = %d, want %d", got, want128)
	}
	// The size test is >=, so 1023 bytes is the most C ever returns:
	// 4 + 7*(8+127) = 949, then 8+66 gives 1023 and 8+67 gives 0.
	fill := func(last int) int {
		s := &scanner{}
		for i := 0; i < 7; i++ {
			s.push(ctxHeredocTemplate).heredocIdentifier = []byte(id127)
		}
		s.push(ctxHeredocTemplate).heredocIdentifier = bytes.Repeat([]byte{'b'}, last)
		return len(serialize(s))
	}
	if got := fill(66); got != 1023 {
		t.Fatalf("fill(66) = %d, want 1023", got)
	}
	if got := fill(67); got != 0 {
		t.Fatalf("fill(67) = %d, want 0", got)
	}
}

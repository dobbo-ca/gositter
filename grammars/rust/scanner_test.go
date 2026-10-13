package rust

import (
	"bytes"
	"testing"

	"github.com/dobbo-ca/gositter"
)

func TestSerializeRoundTrip(t *testing.T) {
	buf := make([]byte, gositter.SerializationBufferSize)
	if n := newScanner().Serialize(buf); !bytes.Equal(buf[:n], []byte{0}) {
		t.Fatalf("fresh scanner = %v, want [0]", buf[:n])
	}
	for i := 0; i < 256; i++ {
		s := &scanner{openingHashCount: uint8(i)}
		n := s.Serialize(buf)
		if !bytes.Equal(buf[:n], []byte{byte(i)}) {
			t.Fatalf("serialize(%d) = %v", i, buf[:n])
		}
		got := &scanner{openingHashCount: uint8(i + 1)}
		got.Deserialize(buf[:n])
		if *got != *s {
			t.Fatalf("round trip %d = %d", i, got.openingHashCount)
		}
	}
}

func TestDeserializeOtherLengths(t *testing.T) {
	// C resets, then reads only when length is exactly 1.
	for _, b := range [][]byte{nil, {}, {7, 7}, {7, 7, 7}} {
		s := &scanner{openingHashCount: 5}
		s.Deserialize(b)
		if s.openingHashCount != 0 {
			t.Fatalf("Deserialize(%v) = %d, want 0", b, s.openingHashCount)
		}
	}
}

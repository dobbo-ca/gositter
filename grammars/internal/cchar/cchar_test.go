package cchar

import "testing"

func TestValue(t *testing.T) {
	want, max := int32(-1), 127
	if Unsigned {
		want, max = 255, 255
	}
	if Value(0xFF) != want || Value(0x7F) != 127 || Max != max {
		t.Fatalf("Unsigned=%v: Value(0xFF)=%d Max=%d", Unsigned, Value(0xFF), Max)
	}
}

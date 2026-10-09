// Package cchar gives scanners the plain char of the C compiler that the
// matching cgo build uses, so a port can follow C where a scanner stores a
// code point in a char and reads it back.
package cchar

// Value is a char read back as an int, as C's integer promotion gives it.
func Value(b byte) int32 {
	if Unsigned {
		return int32(b)
	}
	return int32(int8(b))
}

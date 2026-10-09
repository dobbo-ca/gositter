//go:build !(linux && (arm64 || arm || ppc64 || ppc64le || riscv64 || s390x))

package cchar

// Unsigned is false: char is signed on x86 and on every darwin target.
const Unsigned = false

// Max is CHAR_MAX.
const Max = 127

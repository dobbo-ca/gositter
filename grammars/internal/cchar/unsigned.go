//go:build linux && (arm64 || arm || ppc64 || ppc64le || riscv64 || s390x)

package cchar

// Unsigned is true: the Linux ABI of these targets makes char unsigned.
// Measured with gcc on linux/arm64 only (CHAR_MIN == 0).
const Unsigned = true

// Max is CHAR_MAX.
const Max = 255

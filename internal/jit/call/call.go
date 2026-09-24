//go:build (darwin || linux) && (arm64 || amd64)

// Package call enters generated code.
//
// Generated code receives ctx in the first argument register (X0 on arm64,
// DI on amd64) and returns a result in X0 or AX. It must not use the Go
// stack, call Go, or change the registers Go reserves: on arm64 R18, R26,
// R27, R28 (g), R29 (frame pointer), R30 (link register, used to return)
// and SP; on amd64 SP, BP, R12, R13, R14 (g) and X15, which Go keeps zero.
package call

import "unsafe"

// Call runs the code at entry with ctx and returns its result. ctx must
// stay reachable while the code runs.
//
//go:noescape
func Call(entry uintptr, ctx unsafe.Pointer) uint64

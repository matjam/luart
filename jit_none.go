//go:build !((darwin || linux) && arm64)

package lua

const jitSupported = false

func compileJIT(p *prototype) (code []byte, offsets []int32, entries []int) { return nil, nil, nil }

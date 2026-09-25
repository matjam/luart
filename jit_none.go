//go:build !((darwin || linux) && (arm64 || amd64))

package luart

const jitSupported = false

func compileJIT(p *prototype) (code []byte, offsets []int32, entries []int, kernels int) {
	return nil, nil, nil, 0
}

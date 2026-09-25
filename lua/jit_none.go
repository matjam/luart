//go:build !((darwin || linux) && (arm64 || amd64))

package lua

const jitSupported = false

func compileJIT(p *prototype, g *globalState) (code []byte, offsets []int32, entries []int, kernels int) {
	return nil, nil, nil, 0
}

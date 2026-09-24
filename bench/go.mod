module luabench

go 1.27.1

require (
	github.com/Shopify/go-lua v0.0.0-20250718183320-1e37f32ad7d0
	github.com/arnodel/golua v0.3.0
	github.com/dop251/goja v0.0.0-20260917113740-793a2a65c13b
	github.com/matjam/luart v0.0.0
	github.com/yuin/gopher-lua v1.1.2
)

require (
	github.com/arnodel/strftime v0.1.6 // indirect
	github.com/dlclark/regexp2/v2 v2.5.2 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	golang.org/x/text v0.3.8 // indirect
)

replace github.com/matjam/luart => ../

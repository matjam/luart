package luabench

// The plasma canvas: 200x100 half-block pixels.
const W, H = 200, 100

var canvas [W * H]float64

func set(x, y int, v float64) { canvas[y*W+x] = v }

func setClipped(x, y, v float64) {
	xi, yi := int(x), int(y)
	if xi >= 0 && xi < W && yi >= 0 && yi < H {
		set(xi, yi, v)
	}
}

// One full-screen plasma frame, with a Go call per pixel.
const luaSrc = `
local sin = math.sin
function frame(t)
  for y = 0, 99 do
    for x = 0, 199 do
      set(x, y, sin(x*0.1+t) + sin(y*0.07+t) + sin((x+y)*0.05+t))
    end
  end
end
`

// The same maths, storing results in a Lua table instead of calling Go.
const luaNoCall = `
local sin = math.sin
local buf = {}
function frame(t)
  local i = 1
  for y = 0, 99 do
    for x = 0, 199 do
      buf[i] = sin(x*0.1+t) + sin(y*0.07+t) + sin((x+y)*0.05+t)
      i = i + 1
    end
  end
end
`

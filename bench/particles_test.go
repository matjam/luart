package luabench

import "testing"

// A shape-based visualiser: 2,000 particles in tables, moved and drawn each
// frame. Field access and method calls dominate.
const particlesSrc = `
local Particle = {}
Particle.__index = Particle

function Particle.new(i)
  return setmetatable({x = i % 200, y = i % 100, vx = (i % 7) - 3, vy = (i % 5) - 2, life = 100}, Particle)
end

function Particle:step(dt)
  self.x = self.x + self.vx * dt
  self.y = self.y + self.vy * dt
  if self.x < 0 or self.x >= 200 then self.vx = -self.vx end
  if self.y < 0 or self.y >= 100 then self.vy = -self.vy end
  self.life = self.life - dt
  if self.life <= 0 then self.life = 100 end
end

local particles = {}
for i = 1, 2000 do particles[i] = Particle.new(i) end

function frame(t)
  for i = 1, #particles do
    local p = particles[i]
    p:step(0.5)
    set(p.x, p.y, p.life / 100)
  end
end
`

type particle struct{ x, y, vx, vy, life float64 }

func (p *particle) step(dt float64) {
	p.x += p.vx * dt
	p.y += p.vy * dt
	if p.x < 0 || p.x >= 200 {
		p.vx = -p.vx
	}
	if p.y < 0 || p.y >= 100 {
		p.vy = -p.vy
	}
	p.life -= dt
	if p.life <= 0 {
		p.life = 100
	}
}

func BenchmarkGoParticles(b *testing.B) {
	ps := make([]particle, 2000)
	for i := range ps {
		j := i + 1
		ps[i] = particle{float64(j % 200), float64(j % 100), float64(j%7 - 3), float64(j%5 - 2), 100}
	}
	for b.Loop() {
		for i := range ps {
			ps[i].step(0.5)
			setClipped(ps[i].x, ps[i].y, ps[i].life/100)
		}
	}
}

func BenchmarkLuartParticles(b *testing.B) {
	l := newLuart(b, particlesSrc)
	l.RegisterNumberFunction("set", setClipped)
	runLuartFrames(b, l)
}

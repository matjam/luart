package lua

import "github.com/matjam/luart/internal/bytecode"

const (
	maxStack         = bytecode.MaxStack
	maxCallCount     = bytecode.MaxCallCount
	errorStackSize   = maxStack + 200
	extraStack       = 5
	basicStackSize   = 2 * MinStack
	maxTagLoop       = 100
	firstPseudoIndex = -maxStack - 1000
	maxUpValue       = bytecode.MaxUpValue
	apiCheck         = false
)

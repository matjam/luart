package amd64

import "testing"

func TestHasSSE41(t *testing.T) {
	t.Logf("SSE4.1: %v", HasSSE41())
}

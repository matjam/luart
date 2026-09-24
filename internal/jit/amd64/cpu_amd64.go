package amd64

func cpuid1ECX() uint32

// HasSSE41 reports whether the CPU has SSE4.1, which ROUNDSD needs.
func HasSSE41() bool { return cpuid1ECX()&(1<<19) != 0 }

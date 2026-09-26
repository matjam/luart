package main

import (
	"bytes"
	"os"
	"sync"
)

// A capture takes over os.Stdout and os.Stderr, where print, io.write and
// the commands os.execute and io.popen run write, so that the TUI can put
// their output in its transcript rather than across its screen. Until the
// TUI attaches, output goes straight through to the terminal, in order.
type capture struct {
	terminal *os.File // the real stdout
	errors   *os.File // the real stderr
	r, w     *os.File

	mu      sync.Mutex
	lines   func(string) // the TUI's printer, or nil to write through
	pending []byte       // output after the last newline, while attached

	synced chan struct{}
	done   chan struct{}
}

// syncMark separates output written before Sync from output after it.
var syncMark = []byte("\x00apogee-sync\x00")

func startCapture() (*capture, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	c := &capture{terminal: os.Stdout, errors: os.Stderr, r: r, w: w,
		synced: make(chan struct{}), done: make(chan struct{})}
	os.Stdout, os.Stderr = w, w
	go c.forward()
	return c, nil
}

// attach sends complete lines to lines from now on. Output written before
// attach goes to the terminal.
func (c *capture) attach(lines func(string)) {
	c.Sync()
	c.mu.Lock()
	c.lines = lines
	c.mu.Unlock()
}

// Sync waits until everything written so far has been forwarded, including
// the end of a line not yet finished.
func (c *capture) Sync() {
	c.w.Write(syncMark)
	<-c.synced
}

// detach writes through to the terminal again, with any unfinished line.
// It does not Sync: the TUI's printer only works while the TUI runs, and
// every evaluation ends with a Sync, so nothing is in flight once it stops.
func (c *capture) detach() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = nil
	c.terminal.Write(c.pending)
	c.pending = c.pending[:0]
}

// stop restores os.Stdout and os.Stderr once the output so far is out.
func (c *capture) stop() {
	c.Sync()
	os.Stdout, os.Stderr = c.terminal, c.errors
	c.w.Close()
	<-c.done
}

func (c *capture) forward() {
	defer close(c.done)
	buf := make([]byte, 32<<10)
	var in []byte
	for {
		n, err := c.r.Read(buf)
		in = append(in, buf[:n]...)
		for {
			i := bytes.Index(in, syncMark)
			if i < 0 {
				break
			}
			c.emit(in[:i], true)
			in = in[i+len(syncMark):]
			c.synced <- struct{}{}
		}
		// Keep what might be the start of a mark for the next read.
		keep := 0
		for k := len(syncMark) - 1; k > 0; k-- {
			if bytes.HasSuffix(in, syncMark[:k]) {
				keep = k
				break
			}
		}
		c.emit(in[:len(in)-keep], err != nil)
		in = in[len(in)-keep:]
		if err != nil {
			return
		}
	}
}

// emit forwards b: straight to the terminal, or as complete lines to the
// TUI, holding back an unfinished line unless flush is set.
func (c *capture) emit(b []byte, flush bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lines == nil {
		c.terminal.Write(b)
		return
	}
	c.pending = append(c.pending, b...)
	for {
		i := bytes.IndexByte(c.pending, '\n')
		if i < 0 {
			break
		}
		c.lines(string(c.pending[:i]))
		c.pending = c.pending[i+1:]
	}
	if flush && len(c.pending) > 0 {
		c.lines(string(c.pending))
		c.pending = c.pending[:0]
	}
}

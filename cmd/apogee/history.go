package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
)

// maxHistory is how many entries the history file keeps.
const maxHistory = 1000

// A history is the REPL's past input, oldest first, kept in a file of one
// quoted entry a line so that multi-line entries survive.
type history struct {
	path    string // "" keeps it in memory only
	entries []string
}

// historyPath is where the history lives: $APOGEE_HISTORY, else
// ~/.apogee_history. An empty $APOGEE_HISTORY keeps no file.
func historyPath() string {
	if p, ok := os.LookupEnv("APOGEE_HISTORY"); ok {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".apogee_history")
}

func loadHistory(path string) *history {
	h := &history{path: path}
	if path == "" {
		return h
	}
	f, err := os.Open(path)
	if err != nil {
		return h
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(nil, 1<<20)
	for s.Scan() {
		if e, err := strconv.Unquote(s.Text()); err == nil {
			h.entries = append(h.entries, e)
		}
	}
	h.trim()
	return h
}

// add appends entry, unless it repeats the last one, and saves the file.
func (h *history) add(entry string) {
	if n := len(h.entries); n > 0 && h.entries[n-1] == entry {
		return
	}
	h.entries = append(h.entries, entry)
	h.trim()
	h.save()
}

func (h *history) trim() {
	if n := len(h.entries); n > maxHistory {
		h.entries = h.entries[n-maxHistory:]
	}
}

// save writes the history, quietly doing nothing if it cannot.
func (h *history) save() {
	if h.path == "" {
		return
	}
	tmp := h.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	w := bufio.NewWriter(f)
	for _, e := range h.entries {
		w.WriteString(strconv.Quote(e))
		w.WriteByte('\n')
	}
	if w.Flush() != nil || f.Close() != nil {
		os.Remove(tmp)
		return
	}
	os.Rename(tmp, h.path)
}

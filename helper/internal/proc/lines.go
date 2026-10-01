package proc

import (
	"bytes"
	"sync"
)

// LineWriter splits written bytes into lines (on \n or \r) and calls fn for each
// non-empty line. Progress output from yt-dlp/ffmpeg arrives this way.
type LineWriter struct {
	mu   sync.Mutex
	fn   func(string)
	part []byte
	// Max caps a single line's length; longer data is flushed as-is.
	Max int
}

// NewLineWriter returns a LineWriter calling fn.
func NewLineWriter(fn func(string)) *LineWriter { return &LineWriter{fn: fn, Max: 1 << 20} }

func (w *LineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	data := p
	if len(w.part) > 0 {
		data = append(w.part, p...)
	}
	for {
		i := bytes.IndexAny(data, "\r\n")
		if i < 0 {
			break
		}
		if i > 0 {
			w.fn(string(data[:i]))
		}
		data = data[i+1:]
	}
	if w.Max > 0 && len(data) > w.Max {
		w.fn(string(data))
		data = nil
	}
	w.part = append(w.part[:0], data...)
	return len(p), nil
}

// Flush emits any buffered partial line.
func (w *LineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.part) > 0 {
		w.fn(string(w.part))
		w.part = w.part[:0]
	}
}

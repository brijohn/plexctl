package term

import (
	"io"
	"os"
	"sync"
)

/*
SyncWriter serializes writes to the terminal between the Bubble Tea renderer
(which writes one frame per Write call) and out-of-band escape sequences such
as kitty graphics transmissions. Sharing one SyncWriter via tea.WithOutput
guarantees frames and graphics payloads never interleave mid-sequence.

When wrapping an *os.File it also satisfies Bubble Tea's term.File
(io.ReadWriteCloser + Fd), which Bubble Tea requires on its output to detect
the tty, query the window size, and handle resizes.
*/
type SyncWriter struct {
	mu sync.Mutex
	w  io.Writer
	f  *os.File // non-nil when wrapping a real file
}

func NewSyncWriter(w io.Writer) *SyncWriter {
	s := &SyncWriter{w: w}
	if f, ok := w.(*os.File); ok {
		s.f = f
	}
	return s
}

func (s *SyncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// Locked holds the lock across fn, for multi-write sequences that must not be
// interleaved with other output (e.g. chunked kitty image transmissions).
func (s *SyncWriter) Locked(fn func(io.Writer) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(s.w)
}

// Fd exposes the underlying file descriptor for tty detection and window-size
// queries; an invalid descriptor is returned when no real file is wrapped.
func (s *SyncWriter) Fd() uintptr {
	if s.f != nil {
		return s.f.Fd()
	}
	return ^uintptr(0)
}

func (s *SyncWriter) Read(p []byte) (int, error) {
	if s.f != nil {
		return s.f.Read(p)
	}
	return 0, io.EOF
}

func (s *SyncWriter) Close() error {
	if s.f != nil {
		return s.f.Close()
	}
	return nil
}

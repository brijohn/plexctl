package term

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
)

// Concurrent Write and Locked sequences must never interleave.
func TestSyncWriterNoInterleave(t *testing.T) {
	var buf bytes.Buffer
	sw := NewSyncWriter(&buf)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = sw.Write([]byte("<frame>"))
		}()
		go func() {
			defer wg.Done()
			_ = sw.Locked(func(w io.Writer) error {
				for _, part := range []string{"[a", "b", "c]"} {
					if _, err := io.WriteString(w, part); err != nil {
						return err
					}
				}
				return nil
			})
		}()
	}
	wg.Wait()

	out := buf.String()
	if got := strings.Count(out, "[abc]"); got != 50 {
		t.Errorf("found %d intact locked sequences, want 50", got)
	}
	if got := strings.Count(out, "<frame>"); got != 50 {
		t.Errorf("found %d intact frames, want 50", got)
	}
}

//go:build !windows

package term

import (
	"bytes"
	"os"
	"time"

	"golang.org/x/sys/unix"
	xterm "golang.org/x/term"
)

/*
queryKittyGraphics sends a kitty graphics query (a=q with a 1x1 dummy image)
followed by a DA1 request, then reads the reply in raw mode. A terminal that
supports the protocol answers the query with "_Gi=31;OK" before its DA1
response; terminals that don't still answer DA1, bounding the wait.
*/
func queryKittyGraphics(timeout time.Duration) bool {
	fd := int(os.Stdin.Fd())
	oldState, err := xterm.MakeRaw(fd)
	if err != nil {
		return false
	}
	defer xterm.Restore(fd, oldState) //nolint:errcheck

	if _, err := os.Stdout.WriteString("\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\\x1b[c"); err != nil {
		return false
	}

	deadline := time.Now().Add(timeout)
	var buf []byte
	tmp := make([]byte, 256)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(remaining.Milliseconds())+1)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			break
		}
		if n == 0 {
			break
		}
		nr, err := unix.Read(fd, tmp)
		if nr > 0 {
			buf = append(buf, tmp[:nr]...)
			if hasDA1Response(buf) {
				break
			}
		}
		if err != nil {
			break
		}
	}

	return bytes.Contains(buf, []byte("_Gi=31;OK"))
}

// hasDA1Response reports whether buf contains a complete DA1 reply (CSI ? ... c).
func hasDA1Response(buf []byte) bool {
	idx := bytes.Index(buf, []byte("\x1b[?"))
	if idx < 0 {
		return false
	}
	return bytes.IndexByte(buf[idx:], 'c') >= 0
}

// cellSize returns the terminal cell size in pixels from TIOCGWINSZ, or 0,0
// when the terminal doesn't report pixel dimensions.
func cellSize() (int, int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 || ws.Xpixel == 0 || ws.Ypixel == 0 {
		return 0, 0
	}
	return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
}

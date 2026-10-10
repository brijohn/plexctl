//go:build windows

package term

import "time"

func queryKittyGraphics(_ time.Duration) bool {
	return false
}

func cellSize() (int, int) {
	return 0, 0
}

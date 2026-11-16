package remote

import "path/filepath"

// Gamepads counts the gamepads connected to this machine right now.
func Gamepads() int {
	found, _ := filepath.Glob("/dev/input/js*")
	return len(found)
}

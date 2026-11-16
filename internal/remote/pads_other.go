//go:build !linux && !windows

package remote

// Gamepads counts the gamepads connected to this machine right now. Hosting
// is not available on this system, so nothing needs the count.
func Gamepads() int { return 0 }

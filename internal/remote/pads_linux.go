package remote

import (
	"math/big"
	"os"
	"strings"
)

// Gamepads counts the gamepads connected to this machine right now.
func Gamepads() int {
	b, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		return 0
	}
	return countGamepads(string(b))
}

// Button codes from linux/input-event-codes.h.
const (
	btnJoystick = 0x120
	btnGamepad  = 0x130
)

// countGamepads counts the joystick devices in /proc/bus/input/devices that
// have gamepad or joystick buttons. Sunshine's virtual absolute mouse also
// gets a /dev/input/js node, so counting js nodes alone would take it for
// a guest's pad.
func countGamepads(devices string) int {
	n := 0
	for _, dev := range strings.Split(devices, "\n\n") {
		js, keys := false, ""
		for _, line := range strings.Split(dev, "\n") {
			if h, ok := strings.CutPrefix(line, "H: Handlers="); ok {
				for _, f := range strings.Fields(h) {
					js = js || strings.HasPrefix(f, "js")
				}
			}
			if k, ok := strings.CutPrefix(line, "B: KEY="); ok {
				keys = k
			}
		}
		if !js {
			continue
		}
		bits := keyBits(keys)
		if bits.Bit(btnGamepad) == 1 || bits.Bit(btnJoystick) == 1 {
			n++
		}
	}
	return n
}

// keyBits reads a KEY bitmap: hex words of the kernel's long size, most
// significant first, with leading zeros dropped in each word.
func keyBits(s string) *big.Int {
	words := strings.Fields(s)
	hex := ""
	for i, w := range words {
		if i > 0 {
			w = strings.Repeat("0", 16-len(w)) + w
		}
		hex += w
	}
	bits, ok := new(big.Int).SetString(hex, 16)
	if !ok {
		return new(big.Int)
	}
	return bits
}

package remote

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var xinputGetState = windows.NewLazySystemDLL("xinput1_4.dll").NewProc("XInputGetState")

// Gamepads counts the gamepads connected to this machine right now.
func Gamepads() int {
	if xinputGetState.Find() != nil {
		return 0
	}
	n := 0
	var state [16]byte // XINPUT_STATE
	for i := uintptr(0); i < 4; i++ {
		if ret, _, _ := xinputGetState.Call(i, uintptr(unsafe.Pointer(&state))); ret == 0 {
			n++
		}
	}
	return n
}

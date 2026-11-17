package remote

// A guest without a gamepad can play with the keyboard: their EmuHub reads
// the keys and drives a virtual gamepad on their own computer, which
// Moonlight sends to the host like any other. Every guest decides for
// themselves, each is a separate player, and the host's keyboard stays
// closed to guests.
//
// Keys are named by their Linux key codes (linux/input-event-codes.h);
// other systems translate theirs.

// Pad buttons, in the order an Xbox 360 pad reports them.
const (
	padA = iota
	padB
	padX
	padY
	padLB
	padRB
	padBack
	padStart
	padGuide
	padL3
	padR3
	padButtons
)

// Linux key codes the layout uses.
const (
	keyBackspace = 14
	keyQ         = 16
	keyW         = 17
	keyE         = 18
	keyU         = 22
	keyI         = 23
	keyO         = 24
	keyP         = 25
	keyEnter     = 28
	keyA         = 30
	keyS         = 31
	keyD         = 32
	keyF         = 33
	keyH         = 35
	keyJ         = 36
	keyK         = 37
	keyKP8       = 72
	keyKP4       = 75
	keyKP6       = 77
	keyKP2       = 80
	keyUp        = 103
	keyLeft      = 105
	keyRight     = 106
	keyDown      = 108
)

// KeyBinding is one line of the keyboard layout, for the interface.
type KeyBinding struct {
	Keys   string `json:"keys"`
	Action string `json:"action"`
}

// KeyboardLayout is the keyboard layout, as the join panel shows it.
func KeyboardLayout() []KeyBinding {
	return []KeyBinding{
		{"W A S D", "Stick izquierdo"},
		{"Flechas", "Cruceta"},
		{"J · K · U · I", "A · B · X · Y"},
		{"Q · O", "LB · RB"},
		{"E · P", "LT · RT"},
		{"Teclado numérico 8 4 2 6", "Stick derecho (botones C en N64)"},
		{"Intro · Retroceso", "Start · Select"},
		{"F · H", "Pulsar sticks (L3 · R3)"},
	}
}

var buttonKeys = map[uint16]int{
	keyJ: padA, keyK: padB, keyU: padX, keyI: padY,
	keyQ: padLB, keyO: padRB,
	keyBackspace: padBack, keyEnter: padStart,
	keyF: padL3, keyH: padR3,
}

// padState is everything a pad reports.
type padState struct {
	buttons    [padButtons]bool
	lx, ly     int32 // -32767…32767, up is negative
	rx, ry     int32
	lt, rt     int32 // 0…255
	hatX, hatY int32 // -1, 0 or 1
}

// padFromKeys works out the pad from the keys held down.
func padFromKeys(down map[uint16]bool) padState {
	var s padState
	for k, b := range buttonKeys {
		if down[k] {
			s.buttons[b] = true
		}
	}
	axis := func(neg, pos uint16, full int32) int32 {
		v := int32(0)
		if down[neg] {
			v -= full
		}
		if down[pos] {
			v += full
		}
		return v
	}
	s.lx, s.ly = axis(keyA, keyD, 32767), axis(keyW, keyS, 32767)
	s.rx, s.ry = axis(keyKP4, keyKP6, 32767), axis(keyKP8, keyKP2, 32767)
	s.hatX, s.hatY = axis(keyLeft, keyRight, 1), axis(keyUp, keyDown, 1)
	if down[keyE] {
		s.lt = 255
	}
	if down[keyP] {
		s.rt = 255
	}
	return s
}

// KeyboardPad turns this computer's keyboard into a gamepad until closed.
type KeyboardPad interface {
	Close() error
}

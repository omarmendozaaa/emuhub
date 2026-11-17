package remote

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// On Linux the keys are read from the keyboards' evdev devices, which works
// whatever window has the focus (Moonlight's, in fullscreen), and the pad is
// a uinput device. Both need the user in the input group, as hosting does.

// Event types and codes from linux/input-event-codes.h.
const (
	evSyn     = 0x00
	evKey     = 0x01
	evAbs     = 0x03
	evRep     = 0x14
	synReport = 0

	absX     = 0x00
	absY     = 0x01
	absZ     = 0x02
	absRX    = 0x03
	absRY    = 0x04
	absRZ    = 0x05
	absHat0X = 0x10
	absHat0Y = 0x11
)

// The pad's buttons, as an Xbox 360 pad reports them through xpad: SDL
// numbers them in this order, which its mapping for this pad expects.
var padCodes = [padButtons]uint16{
	padA: 0x130, padB: 0x131, padX: 0x133, padY: 0x134,
	padLB: 0x136, padRB: 0x137,
	padBack: 0x13a, padStart: 0x13b, padGuide: 0x13c,
	padL3: 0x13d, padR3: 0x13e,
}

// ioctls from linux/uinput.h.
const (
	uiDevCreate  = 0x5501
	uiDevDestroy = 0x5502
	uiSetEvBit   = 0x40045564
	uiSetKeyBit  = 0x40045565
	uiSetAbsBit  = 0x40045567
)

// padName is how the pad appears on the guest's computer.
const padName = "EmuHub teclado (X-Box 360 pad)"

// KeyboardPadSupported reports whether this computer can play with the
// keyboard, and why not.
func KeyboardPadSupported() error {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		return errNoInputAccess
	}
	f.Close()
	if len(keyboardDevices()) == 0 {
		if _, err := os.Stat("/dev/input"); err == nil {
			return errNoInputAccess
		}
		return errors.New("no se encontró ningún teclado")
	}
	return nil
}

var errNoInputAccess = errors.New("EmuHub no puede leer el teclado ni crear el mando: tu usuario no tiene acceso a los dispositivos de entrada. Ejecuta «sudo usermod -aG input $USER» y vuelve a iniciar sesión")

// StartKeyboardPad creates the pad and starts following the keyboards.
func StartKeyboardPad() (KeyboardPad, error) {
	out, err := createPad()
	if err != nil {
		return nil, err
	}
	p := &linuxPad{out: out, down: map[uint16]bool{}, keyboards: map[string]*os.File{}, done: make(chan struct{})}
	if p.scan() == 0 {
		p.Close()
		return nil, errNoInputAccess
	}
	go p.rescan()
	return p, nil
}

type linuxPad struct {
	out *os.File

	mu        sync.Mutex
	down      map[uint16]bool
	last      padState
	keyboards map[string]*os.File // by device path
	closed    bool

	done      chan struct{}
	closeOnce sync.Once
}

// scan opens keyboards not open yet and returns how many are open.
func (p *linuxPad) scan() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0
	}
	for _, path := range keyboardDevices() {
		if _, ok := p.keyboards[path]; ok {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		p.keyboards[path] = f
		go p.read(path, f)
	}
	return len(p.keyboards)
}

// rescan picks up keyboards plugged in during the session.
func (p *linuxPad) rescan() {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-tick.C:
			p.scan()
		}
	}
}

// inputEvent is struct input_event on 64-bit Linux.
type inputEvent struct {
	Sec, Usec int64
	Type      uint16
	Code      uint16
	Value     int32
}

const eventSize = int(unsafe.Sizeof(inputEvent{}))

func (p *linuxPad) read(path string, f *os.File) {
	defer func() {
		p.mu.Lock()
		if p.keyboards[path] == f {
			delete(p.keyboards, path)
		}
		p.mu.Unlock()
		f.Close()
	}()
	buf := make([]byte, eventSize*64)
	for {
		n, err := f.Read(buf)
		if err != nil {
			return // unplugged, or closed by Close
		}
		changed := false
		p.mu.Lock()
		for i := 0; i+eventSize <= n; i += eventSize {
			var ev inputEvent
			binary.Read(bytes.NewReader(buf[i:i+eventSize]), binary.NativeEndian, &ev)
			// 1 is a press, 0 a release; repeats (2) change nothing.
			if ev.Type != evKey || ev.Value == 2 {
				continue
			}
			if ev.Value == 1 {
				p.down[ev.Code] = true
			} else {
				delete(p.down, ev.Code)
			}
			changed = true
		}
		if changed {
			p.emit(padFromKeys(p.down))
		}
		p.mu.Unlock()
	}
}

// emit writes what changed since the last state. Called with p.mu held.
func (p *linuxPad) emit(s padState) {
	if p.closed {
		return
	}
	var evs []inputEvent
	for i, code := range padCodes {
		if s.buttons[i] != p.last.buttons[i] {
			v := int32(0)
			if s.buttons[i] {
				v = 1
			}
			evs = append(evs, inputEvent{Type: evKey, Code: code, Value: v})
		}
	}
	axes := []struct {
		code      uint16
		now, prev int32
	}{
		{absX, s.lx, p.last.lx}, {absY, s.ly, p.last.ly},
		{absRX, s.rx, p.last.rx}, {absRY, s.ry, p.last.ry},
		{absZ, s.lt, p.last.lt}, {absRZ, s.rt, p.last.rt},
		{absHat0X, s.hatX, p.last.hatX}, {absHat0Y, s.hatY, p.last.hatY},
	}
	for _, a := range axes {
		if a.now != a.prev {
			evs = append(evs, inputEvent{Type: evAbs, Code: a.code, Value: a.now})
		}
	}
	if len(evs) == 0 {
		return
	}
	evs = append(evs, inputEvent{Type: evSyn, Code: synReport})
	var b bytes.Buffer
	binary.Write(&b, binary.NativeEndian, evs)
	p.out.Write(b.Bytes())
	p.last = s
}

func (p *linuxPad) Close() error {
	p.closeOnce.Do(func() {
		close(p.done)
		p.mu.Lock()
		p.closed = true
		for _, f := range p.keyboards {
			f.Close()
		}
		p.mu.Unlock()
		unix.IoctlSetInt(int(p.out.Fd()), uiDevDestroy, 0)
		p.out.Close()
	})
	return nil
}

// uinputUserDev is struct uinput_user_dev.
type uinputUserDev struct {
	Name         [80]byte
	Bustype      uint16
	Vendor       uint16
	Product      uint16
	Version      uint16
	FFEffectsMax uint32
	AbsMax       [64]int32
	AbsMin       [64]int32
	AbsFuzz      [64]int32
	AbsFlat      [64]int32
}

// createPad makes a uinput device that looks like a wired Xbox 360 pad, the
// one SDL, and so Moonlight, maps without guessing.
func createPad() (*os.File, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errNoInputAccess
	}
	fd := int(f.Fd())
	fail := func(err error) (*os.File, error) {
		f.Close()
		return nil, fmt.Errorf("no se pudo crear el mando del teclado: %w", err)
	}
	for _, ev := range []int{evKey, evAbs, evSyn} {
		if err := unix.IoctlSetInt(fd, uiSetEvBit, ev); err != nil {
			return fail(err)
		}
	}
	for _, code := range padCodes {
		if err := unix.IoctlSetInt(fd, uiSetKeyBit, int(code)); err != nil {
			return fail(err)
		}
	}
	dev := uinputUserDev{Bustype: 0x03, Vendor: 0x045e, Product: 0x028e, Version: 0x0114}
	copy(dev.Name[:], padName)
	for _, a := range []struct {
		code     int
		min, max int32
	}{
		{absX, -32768, 32767}, {absY, -32768, 32767},
		{absRX, -32768, 32767}, {absRY, -32768, 32767},
		{absZ, 0, 255}, {absRZ, 0, 255},
		{absHat0X, -1, 1}, {absHat0Y, -1, 1},
	} {
		if err := unix.IoctlSetInt(fd, uiSetAbsBit, a.code); err != nil {
			return fail(err)
		}
		dev.AbsMin[a.code], dev.AbsMax[a.code] = a.min, a.max
	}
	var b bytes.Buffer
	binary.Write(&b, binary.NativeEndian, dev)
	if _, err := f.Write(b.Bytes()); err != nil {
		return fail(err)
	}
	if err := unix.IoctlSetInt(fd, uiDevCreate, 0); err != nil {
		return fail(err)
	}
	return f, nil
}

// keyboardDevices lists the event devices of keyboards: those with the kbd
// handler that repeat keys, which leaves out power buttons and the like.
func keyboardDevices() []string {
	b, err := os.ReadFile("/proc/bus/input/devices")
	if err != nil {
		return nil
	}
	return parseKeyboards(string(b))
}

func parseKeyboards(devices string) []string {
	var out []string
	for _, dev := range strings.Split(devices, "\n\n") {
		kbd, event, repeats, ours := false, "", false, false
		for _, line := range strings.Split(dev, "\n") {
			if name, ok := strings.CutPrefix(line, "N: Name="); ok {
				ours = strings.Contains(name, padName)
			}
			if h, ok := strings.CutPrefix(line, "H: Handlers="); ok {
				for _, f := range strings.Fields(h) {
					kbd = kbd || f == "kbd"
					if strings.HasPrefix(f, "event") {
						event = f
					}
				}
			}
			if ev, ok := strings.CutPrefix(line, "B: EV="); ok {
				repeats = keyBits(ev).Bit(evRep) == 1
			}
		}
		if kbd && repeats && event != "" && !ours {
			out = append(out, filepath.Join("/dev/input", event))
		}
	}
	return out
}

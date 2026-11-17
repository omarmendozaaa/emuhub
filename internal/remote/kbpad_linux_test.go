package remote

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestParseKeyboards(t *testing.T) {
	devices := `I: Bus=0019 Vendor=0000 Product=0001 Version=0000
N: Name="Power Button"
H: Handlers=kbd event1
B: EV=3

I: Bus=0003 Vendor=1532 Product=0257 Version=0111
N: Name="Razer Razer Huntsman Mini"
H: Handlers=sysrq kbd leds event2
B: EV=120013

I: Bus=0003 Vendor=045e Product=028e Version=0114
N: Name="EmuHub teclado (X-Box 360 pad)"
H: Handlers=kbd event20 js0
B: EV=10000b

I: Bus=0003 Vendor=046d Product=c547 Version=0111
N: Name="Logitech USB Receiver Mouse"
H: Handlers=mouse0 event5
B: EV=17
`
	got := parseKeyboards(devices)
	if len(got) != 1 || got[0] != "/dev/input/event2" {
		t.Errorf("got %v", got)
	}
}

func TestPadFromKeys(t *testing.T) {
	s := padFromKeys(map[uint16]bool{keyW: true, keyD: true, keyJ: true, keyLeft: true, keyP: true, keyKP2: true})
	if s.lx != 32767 || s.ly != -32767 || !s.buttons[padA] || s.hatX != -1 || s.rt != 255 || s.ry != 32767 || s.lt != 0 {
		t.Errorf("%+v", s)
	}
	// Opposite keys cancel out.
	if s := padFromKeys(map[uint16]bool{keyA: true, keyD: true}); s.lx != 0 {
		t.Errorf("%+v", s)
	}
}

// fakeKeyboard is a uinput keyboard the test types on.
func fakeKeyboard(t *testing.T) *os.File {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Skip("no /dev/uinput access")
	}
	fd := int(f.Fd())
	for _, ev := range []int{evKey, evRep, evSyn} {
		unix.IoctlSetInt(fd, uiSetEvBit, ev)
	}
	for k := 1; k < 128; k++ {
		unix.IoctlSetInt(fd, uiSetKeyBit, k)
	}
	dev := uinputUserDev{Bustype: 0x06, Vendor: 1, Product: 1, Version: 1}
	copy(dev.Name[:], "EmuHub test keyboard")
	var b bytes.Buffer
	binary.Write(&b, binary.NativeEndian, dev)
	f.Write(b.Bytes())
	if err := unix.IoctlSetInt(fd, uiDevCreate, 0); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { unix.IoctlSetInt(fd, uiDevDestroy, 0); f.Close() })
	return f
}

func press(f *os.File, code uint16, v int32) {
	var b bytes.Buffer
	binary.Write(&b, binary.NativeEndian, []inputEvent{{Type: evKey, Code: code, Value: v}, {Type: evSyn}})
	f.Write(b.Bytes())
}

// padDevice finds the event node of the keyboard pad.
func padDevice(t *testing.T) string {
	for range 50 {
		b, _ := os.ReadFile("/proc/bus/input/devices")
		for _, dev := range strings.Split(string(b), "\n\n") {
			if !strings.Contains(dev, padName) {
				continue
			}
			for _, f := range strings.Fields(strings.ReplaceAll(dev, "=", " ")) {
				if strings.HasPrefix(f, "event") {
					return filepath.Join("/dev/input", f)
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("pad device not found")
	return ""
}

func TestKeyboardPad(t *testing.T) {
	kb := fakeKeyboard(t)
	time.Sleep(500 * time.Millisecond) // let udev give the keyboard its node
	pad, err := StartKeyboardPad()
	if err != nil {
		t.Skip(err)
	}
	defer pad.Close()

	// The pad counts as a gamepad on this computer, as Moonlight will see it.
	if host, _ := readPads(); host == 0 {
		t.Error("the pad is not seen as a gamepad")
	}

	node := padDevice(t)
	// udev gives the node its permissions a moment after it appears.
	var in *os.File
	for range 50 {
		if in, err = os.Open(node); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	time.Sleep(300 * time.Millisecond)

	press(kb, keyJ, 1) // A
	press(kb, keyW, 1) // stick up
	press(kb, keyJ, 0)

	want := []inputEvent{
		{Type: evKey, Code: padCodes[padA], Value: 1},
		{Type: evAbs, Code: absY, Value: -32767},
		{Type: evKey, Code: padCodes[padA], Value: 0},
	}
	var got []inputEvent
	in.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, eventSize*16)
	for len(got) < len(want) {
		n, err := in.Read(buf)
		if err != nil {
			break
		}
		for i := 0; i+eventSize <= n; i += eventSize {
			var ev inputEvent
			binary.Read(bytes.NewReader(buf[i:i+eventSize]), binary.NativeEndian, &ev)
			if ev.Type != evSyn {
				ev.Sec, ev.Usec = 0, 0
				got = append(got, ev)
			}
		}
	}
	if len(got) < len(want) {
		t.Fatalf("got %+v", got)
	}
	// Keys read together come out in one report, buttons before axes.
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || g == w
		}
		if !found {
			t.Errorf("missing %+v in %+v", w, got)
		}
	}
	press(kb, keyW, 0)
}

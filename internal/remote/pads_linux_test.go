package remote

import "testing"

func TestCountGamepads(t *testing.T) {
	devices := `I: Bus=0003 Vendor=2dc8 Product=6012 Version=0110
N: Name="8BitDo Ultimate 2 Wireless Controller"
H: Handlers=event24 js0 
B: EV=1b
B: KEY=7cdb000000000000 0 0 0 0

I: Bus=0003 Vendor=1209 Product=0003 Version=0001
N: Name="libvirtualhid Mouse (Absolute)"
H: Handlers=event27 js1 mouse4 
B: KEY=1f0000 0 0 0 0

I: Bus=0003 Vendor=045e Product=02ea Version=0408
N: Name="Sunshine (libvirtualhid) X-Box One Controller"
H: Handlers=event30 js2 
B: KEY=7cdb000000000000 0 0 0 0

N: Name="Razer Razer Huntsman Mini Keyboard"
H: Handlers=sysrq kbd event3 
B: KEY=1000000000007 ff9f207ac14057ff febeffdfffefffff fffffffffffffffe
`
	// The virtual mouse is not a gamepad.
	if host, guests := countGamepads(devices); host != 1 || guests != 1 {
		t.Fatalf("got %d host and %d guest gamepads, want 1 and 1", host, guests)
	}
}

package remote

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, len(s))
	for i, a := range s {
		out[i] = netip.MustParseAddr(a)
	}
	return out
}

func TestInviteRoundTrip(t *testing.T) {
	for _, in := range []Invite{
		{Addrs: addrs("192.168.1.20"), Port: 47989, PIN: "0007"},
		{Addrs: addrs("192.168.1.20", "100.64.0.3", "203.0.113.9"), Port: 48089, PIN: "9999"},
	} {
		code, err := in.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(code, "EH-") {
			t.Errorf("code %q lacks the prefix", code)
		}
		got, err := DecodeInvite(code)
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		if !reflect.DeepEqual(got, in) {
			t.Errorf("got %+v, want %+v", got, in)
		}
		// Codes survive being retyped by hand.
		messy := " " + strings.ToLower(strings.ReplaceAll(code, "-", " ")) + "\n"
		messy = strings.NewReplacer("0", "o", "1", "l").Replace(messy)
		if got, err := DecodeInvite(messy); err != nil || !reflect.DeepEqual(got, in) {
			t.Errorf("retyped %q: got %+v, %v", messy, got, err)
		}
	}
}

func TestInviteRejectsDamage(t *testing.T) {
	code, err := Invite{Addrs: addrs("10.0.0.2"), Port: 47989, PIN: "1234"}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	last := code[len(code)-1]
	swap := byte('7')
	if last == swap {
		swap = '8'
	}
	for _, bad := range []string{"", "hola", "EH-", code[:len(code)-2], code[:len(code)-1] + string(swap), strings.Replace(code, "EH", "XX", 1)} {
		if _, err := DecodeInvite(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	for _, in := range []Invite{
		{Port: 1, PIN: "1234"},
		{Addrs: addrs("10.0.0.2"), Port: 1, PIN: "12a4"},
		{Addrs: addrs("::1"), Port: 1, PIN: "1234"},
	} {
		if _, err := in.Encode(); err == nil {
			t.Errorf("%+v was encoded", in)
		}
	}
}

func TestInviteAddrs(t *testing.T) {
	local := addrs("192.168.1.2", "10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5", "10.0.0.6")
	got := inviteAddrs(local, netip.MustParseAddr("203.0.113.9"), true)
	if len(got) != maxAddrs || got[0] != local[0] || got[len(got)-1] != netip.MustParseAddr("203.0.113.9") {
		t.Errorf("got %v", got)
	}
	// A machine with a public address on its own interface lists it once.
	got = inviteAddrs(addrs("203.0.113.9"), netip.MustParseAddr("203.0.113.9"), true)
	if len(got) != 1 {
		t.Errorf("got %v", got)
	}
	if got = inviteAddrs(local[:2], netip.Addr{}, false); !reflect.DeepEqual(got, local[:2]) {
		t.Errorf("got %v", got)
	}
}

func TestCaptureMethod(t *testing.T) {
	cases := []struct{ wayland, desktop, want string }{
		{"wayland-1", "Hyprland", "wlr"},
		{"wayland-0", "sway:wlroots", "wlr"},
		{"wayland-0", "GNOME", ""},
		{"", "Hyprland", ""},
	}
	for _, c := range cases {
		if got := captureMethod(c.wayland, c.desktop); got != c.want {
			t.Errorf("%+v: got %q", c, got)
		}
	}
}

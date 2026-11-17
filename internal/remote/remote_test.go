package remote

import (
	"net/netip"
	"os"
	"path/filepath"
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

func TestForgetOtherHosts(t *testing.T) {
	const general = "[General]\nuniqueid=abc\n\n"
	const hosts = "[hosts]\n1\\hostname=EmuHub\n1\\uuid=AAAA-1111\nsize=1\n\n"
	const after = "[other]\nkey=1\n"
	file := filepath.Join(t.TempDir(), "Moonlight.conf")
	write := func() {
		if err := os.WriteFile(file, []byte(general+hosts+after), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func() string {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	// The same room keeps its pairing.
	write()
	if err := forgetOtherHosts(file, "aaaa-1111"); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != general+hosts+after {
		t.Errorf("la sala actual se olvidó:\n%s", got)
	}

	// A host saved under another identifier is dropped, the rest stays.
	if err := forgetOtherHosts(file, "BBBB-2222"); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != general+after {
		t.Errorf("quedaron salas antiguas:\n%s", got)
	}

	// Nothing to do without a settings file or an identifier.
	if err := forgetOtherHosts(filepath.Join(t.TempDir(), "no.conf"), "x"); err != nil {
		t.Error(err)
	}
	if err := forgetOtherHosts("", "x"); err != nil {
		t.Error(err)
	}
}

func TestSeats(t *testing.T) {
	if got := GuestSeats(4, false); got != 4 {
		t.Errorf("solo hosteo, 4 jugadores: %d invitados", got)
	}
	if got := GuestSeats(4, true); got != 3 {
		t.Errorf("anfitrión juega, 4 jugadores: %d invitados", got)
	}
	if got := ClampPlayers(1, true, 4); got != 2 {
		t.Errorf("un anfitrión que juega necesita un invitado: %d", got)
	}
	if got := ClampPlayers(9, false, 2); got != 2 {
		t.Errorf("no más jugadores de los que admite el juego: %d", got)
	}

	seats := NewSeats(3, true)
	if !SeatGuest(seats, 1) || !SeatGuest(seats, 2) || SeatGuest(seats, 3) {
		t.Fatalf("asientos mal repartidos: %+v", seats)
	}
	want := []Seat{{Kind: "host"}, {Kind: "guest", Guest: 1}, {Kind: "guest", Guest: 2}}
	if !reflect.DeepEqual(seats, want) {
		t.Fatalf("got %+v", seats)
	}
	if pos := MoveSeat(seats, 2, -1); pos != 1 || seats[1].Guest != 2 || seats[2].Guest != 1 {
		t.Errorf("mover no intercambió: pos %d, %+v", pos, seats)
	}
	if pos := MoveSeat(seats, 0, -1); pos != 0 {
		t.Errorf("mover fuera de la sala: %d", pos)
	}
}

func TestPadIndexes(t *testing.T) {
	// The host plays with a pad: it is the first one, guests follow.
	seats := []Seat{{Kind: "guest", Guest: 2}, {Kind: "host"}, {Kind: "guest", Guest: 1}}
	if got := PadIndexes(seats, 1); !reflect.DeepEqual(got, []int{2, 0, 1}) {
		t.Errorf("con mando del anfitrión: %v", got)
	}
	// The host plays on the keyboard.
	if got := PadIndexes(seats, 0); !reflect.DeepEqual(got, []int{1, -1, 0}) {
		t.Errorf("anfitrión con teclado: %v", got)
	}
	// The host only hosts but has a pad plugged in: guests skip it, and free
	// seats go to the guests still to come.
	seats = []Seat{{Kind: "guest", Guest: 1}, {}, {}}
	if got := PadIndexes(seats, 1); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("solo hosteo: %v", got)
	}
}

func TestMarkReady(t *testing.T) {
	seats := []Seat{{Kind: "host"}, {Kind: "guest", Guest: 1}, {Kind: "guest", Guest: 2}, {}}
	got := MarkReady(seats, 0, 1)
	want := []Seat{
		{Kind: "host", Ready: true, Input: "teclado"},
		{Kind: "guest", Guest: 1, Ready: true, Input: "mando"},
		{Kind: "guest", Guest: 2},
		{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if AllReady(got) {
		t.Fatal("guest 2 has no gamepad yet")
	}
	got = MarkReady(seats, 1, 2)
	if got[0].Input != "mando" || !AllReady(got) {
		t.Fatalf("everyone seated should be ready: %+v", got)
	}
	// A guest who leaves takes their gamepad with them.
	if AllReady(MarkReady(got, 1, 1)) {
		t.Fatal("a guest without gamepad is not ready")
	}
	if AllReady([]Seat{{}, {}}) {
		t.Fatal("nobody to play")
	}
}

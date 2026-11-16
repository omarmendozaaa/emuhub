// Package remote lets two EmuHub users play together: the host streams the
// game with Sunshine and the guest watches and plays through Moonlight, so
// only the host needs the game.
package remote

import (
	"encoding/base32"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// Invite is everything a guest needs to reach and pair with a host. It
// travels as a room code the host shares over any chat.
type Invite struct {
	Addrs []netip.Addr // IPv4 addresses to try, in order of preference
	Port  uint16       // Sunshine's base (HTTP) port
	PIN   string       // 4 digit pairing PIN, valid for this session only
}

const (
	codeVersion = 1
	codePrefix  = "EH"
	maxAddrs    = 6
)

// Crockford's alphabet: no I, L, O or U, so codes survive being read aloud.
var codeEncoding = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

var errBadCode = errors.New("el código de sala no es válido; cópialo de nuevo")

// Encode renders the invite as a code like EH-1A2B-3C4D-….
func (in Invite) Encode() (string, error) {
	if len(in.Addrs) == 0 || len(in.Addrs) > maxAddrs {
		return "", fmt.Errorf("la invitación necesita entre 1 y %d direcciones", maxAddrs)
	}
	pin, err := parsePIN(in.PIN)
	if err != nil {
		return "", err
	}
	b := []byte{codeVersion, byte(in.Port >> 8), byte(in.Port), byte(pin >> 8), byte(pin), byte(len(in.Addrs))}
	for _, a := range in.Addrs {
		if !a.Is4() {
			return "", fmt.Errorf("dirección no admitida: %s", a)
		}
		ip := a.As4()
		b = append(b, ip[:]...)
	}
	b = append(b, checksum(b))
	raw := codeEncoding.EncodeToString(b)
	var sb strings.Builder
	sb.WriteString(codePrefix)
	for i := 0; i < len(raw); i += 4 {
		sb.WriteByte('-')
		sb.WriteString(raw[i:min(i+4, len(raw))])
	}
	return sb.String(), nil
}

// DecodeInvite parses a room code, forgiving spaces, dashes, lower case and
// the letters people confuse with digits.
func DecodeInvite(code string) (Invite, error) {
	s := strings.ToUpper(code)
	s = strings.NewReplacer(" ", "", "-", "", "\n", "", "\t", "", "\r", "").Replace(s)
	s, ok := strings.CutPrefix(s, codePrefix)
	if !ok {
		return Invite{}, errBadCode
	}
	s = strings.NewReplacer("O", "0", "I", "1", "L", "1", "U", "V").Replace(s)
	b, err := codeEncoding.DecodeString(s)
	if err != nil || len(b) < 7 || checksum(b[:len(b)-1]) != b[len(b)-1] {
		return Invite{}, errBadCode
	}
	if b[0] != codeVersion {
		return Invite{}, errors.New("el código es de otra versión de EmuHub; actualizad los dos a la misma")
	}
	n := int(b[5])
	if n == 0 || n > maxAddrs || len(b) != 6+4*n+1 {
		return Invite{}, errBadCode
	}
	in := Invite{
		Port: uint16(b[1])<<8 | uint16(b[2]),
		PIN:  fmt.Sprintf("%04d", int(b[3])<<8|int(b[4])),
	}
	if len(in.PIN) != 4 || in.Port == 0 {
		return Invite{}, errBadCode
	}
	for i := range n {
		in.Addrs = append(in.Addrs, netip.AddrFrom4([4]byte(b[6+4*i:10+4*i])))
	}
	return in, nil
}

func parsePIN(pin string) (int, error) {
	if len(pin) != 4 {
		return 0, errors.New("el PIN debe tener 4 dígitos")
	}
	n := 0
	for _, c := range pin {
		if c < '0' || c > '9' {
			return 0, errors.New("el PIN debe tener 4 dígitos")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// checksum catches typos; it is not a security measure.
func checksum(b []byte) byte {
	var sum byte = 0x5a
	for i, c := range b {
		sum = sum<<1 | sum>>7
		sum ^= c + byte(i)
	}
	return sum
}

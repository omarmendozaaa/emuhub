// Package remote lets two EmuHub users play together: the host streams the
// game with Sunshine and the guest watches and plays through Moonlight, so
// only the host needs the game.
package remote

import (
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// Invite is everything a guest needs to reach and pair with a host. It
// travels as a room code the host shares over any chat.
type Invite struct {
	Addrs []netip.Addr // IPv4 addresses to try, in order of preference
	Port  uint16       // Sunshine's base (HTTP) port
	PIN   string       // 4 digit pairing PIN, valid for this session only
	// Secret names the room on the signaling server and authenticates the
	// tunnel through it. Empty when the host offers no tunnel.
	Secret []byte
	// Server is the signaling server where the tunnel is set up, with its
	// key. Present exactly when Secret is.
	Server string
}

const (
	codeVersion = 2
	codePrefix  = "EH"
	maxAddrs    = 6
	// secretLen is the size of the tunnel secret in bytes.
	secretLen = 8
)

// Crockford's alphabet: no I, L, O or U, so codes survive being read aloud.
var codeEncoding = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

var errBadCode = errors.New("el código de sala no es válido; cópialo de nuevo")

// Share is what the host sends: a link to the room when it has a signaling
// server, which opens EmuHub straight into the room, or else a code.
func (in Invite) Share() (string, error) {
	if in.Server != "" {
		return in.Link()
	}
	return in.Encode()
}

// Encode renders the invite as a code like EH-1A2B-3C4D-….
func (in Invite) Encode() (string, error) {
	b, err := in.payload()
	if err != nil {
		return "", err
	}
	raw := codeEncoding.EncodeToString(b)
	var sb strings.Builder
	sb.WriteString(codePrefix)
	for i := 0; i < len(raw); i += 4 {
		sb.WriteByte('-')
		sb.WriteString(raw[i:min(i+4, len(raw))])
	}
	return sb.String(), nil
}

// Link renders the invite as a link to the page the signaling server serves
// at /j/: it opens EmuHub through the emuhub:// scheme, or tells the guest
// what to do. The link carries the whole invite, so nothing is stored on the
// server. Links are clicked rather than typed, so they use base64 (6 bits per
// character) instead of the code's case-insensitive base32.
func (in Invite) Link() (string, error) {
	if in.Server == "" {
		return "", errors.New("solo las salas con servidor tienen enlace")
	}
	b, err := in.payload()
	if err != nil {
		return "", err
	}
	u, err := url.Parse(strings.TrimSpace(in.Server))
	if err != nil {
		return "", err
	}
	u.RawQuery, u.Fragment = "", ""
	u.Path = strings.TrimRight(u.Path, "/") + linkPath + base64.RawURLEncoding.EncodeToString(b)
	return u.String(), nil
}

// linkPath and linkScheme are where an invite sits in a room link and in the
// link that opens EmuHub.
const (
	linkPath   = "/j/"
	linkScheme = "emuhub://join/"
)

func (in Invite) payload() ([]byte, error) {
	if len(in.Addrs) == 0 || len(in.Addrs) > maxAddrs {
		return nil, fmt.Errorf("la invitación necesita entre 1 y %d direcciones", maxAddrs)
	}
	pin, err := parsePIN(in.PIN)
	if err != nil {
		return nil, err
	}
	if len(in.Secret) != 0 && len(in.Secret) != secretLen {
		return nil, fmt.Errorf("el secreto del túnel debe tener %d bytes", secretLen)
	}
	var server []byte
	if len(in.Secret) > 0 {
		if server, err = packServer(in.Server); err != nil {
			return nil, err
		}
	} else if in.Server != "" {
		return nil, errors.New("un servidor de salas necesita el secreto del túnel")
	}
	b := []byte{codeVersion, byte(in.Port >> 8), byte(in.Port), byte(pin >> 8), byte(pin), byte(len(in.Addrs))}
	for _, a := range in.Addrs {
		if !a.Is4() {
			return nil, fmt.Errorf("dirección no admitida: %s", a)
		}
		ip := a.As4()
		b = append(b, ip[:]...)
	}
	b = append(b, in.Secret...)
	b = append(b, server...)
	return append(b, checksum(b)), nil
}

// DecodeInvite parses a room code or link: EH-… codes, https://…/j/… room
// links and the emuhub://join/… links they open.
func DecodeInvite(code string) (Invite, error) {
	code = strings.TrimSpace(code)
	if token, ok := linkToken(code); ok {
		if strings.HasPrefix(strings.ToUpper(token), codePrefix) {
			return decodeCode(token)
		}
		b, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			return Invite{}, errBadCode
		}
		return parsePayload(b)
	}
	return decodeCode(code)
}

// linkToken finds the invite in a room link or an emuhub:// link.
func linkToken(s string) (string, bool) {
	var token string
	if rest, ok := cutPrefixFold(s, linkScheme); ok {
		token = rest
	} else if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", false
		}
		i := strings.LastIndex(u.Path, linkPath)
		if i < 0 {
			return "", false
		}
		token = u.Path[i+len(linkPath):]
	} else {
		return "", false
	}
	token, _, _ = strings.Cut(token, "?")
	token, _, _ = strings.Cut(token, "#")
	return strings.Trim(token, "/ "), true
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return s, false
}

// decodeCode parses an EH-… code, forgiving spaces, dashes, lower case and
// the letters people confuse with digits.
func decodeCode(code string) (Invite, error) {
	s := strings.ToUpper(code)
	s = strings.NewReplacer(" ", "", "-", "", "\n", "", "\t", "", "\r", "").Replace(s)
	s, ok := strings.CutPrefix(s, codePrefix)
	if !ok {
		return Invite{}, errBadCode
	}
	s = strings.NewReplacer("O", "0", "I", "1", "L", "1", "U", "V").Replace(s)
	b, err := codeEncoding.DecodeString(s)
	if err != nil {
		return Invite{}, errBadCode
	}
	return parsePayload(b)
}

func parsePayload(b []byte) (Invite, error) {
	if len(b) < 7 || checksum(b[:len(b)-1]) != b[len(b)-1] {
		return Invite{}, errBadCode
	}
	if b[0] != codeVersion {
		return Invite{}, errors.New("el código es de otra versión de EmuHub; actualizad los dos a la misma")
	}
	n := int(b[5])
	if n == 0 || n > maxAddrs {
		return Invite{}, errBadCode
	}
	end := 6 + 4*n
	body := len(b) - 1
	if body != end && body < end+secretLen+2 {
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
	if body > end {
		in.Secret = append([]byte(nil), b[end:end+secretLen]...)
		server, used, err := unpackServer(b[end+secretLen : body])
		if err != nil || end+secretLen+used != body {
			return Invite{}, errBadCode
		}
		in.Server = server
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

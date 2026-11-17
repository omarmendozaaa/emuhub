package remote

import (
	"errors"
	"net/url"
	"strings"
)

// The signaling server travels inside the room code, so guests need no
// settings of their own. It is packed to keep codes short: https:// is
// implied, the usual Cloudflare address shrinks to the account's subdomain
// and a hexadecimal key takes half the room.
//
//	flags, len(host), host, [len(key), key]

const (
	srvInsecure  = 1 << 0 // http:// rather than https://
	srvWorkers   = 1 << 1 // host is <host>.workers.dev
	srvDefault   = 1 << 2 // host is emuhub-signal.<host>.workers.dev
	srvKey       = 1 << 3 // a key follows
	srvKeyHex    = 1 << 4 // the key is lowercase hex, stored as bytes
	workersDev   = ".workers.dev"
	defaultName  = "emuhub-signal."
	maxServerLen = 255
)

func packServer(server string) ([]byte, error) {
	u, err := parseServer(server)
	if err != nil {
		return nil, err
	}
	var flags byte
	if u.Scheme == "ws" {
		flags |= srvInsecure
	}
	host := strings.ToLower(u.Host)
	path := strings.TrimRight(u.Path, "/")
	if path == "" && strings.HasSuffix(host, workersDev) {
		host = strings.TrimSuffix(host, workersDev)
		flags |= srvWorkers
		if rest, ok := strings.CutPrefix(host, defaultName); ok && rest != "" {
			host = rest
			flags |= srvDefault
		}
	} else {
		host += path
	}
	key := []byte(u.Query().Get("key"))
	if len(key) > 0 {
		flags |= srvKey
		if packed, ok := packHex(string(key)); ok {
			key = packed
			flags |= srvKeyHex
		}
	}
	if len(host) > maxServerLen || len(key) > maxServerLen {
		return nil, errors.New("la dirección del servidor de salas es demasiado larga")
	}
	b := append([]byte{flags, byte(len(host))}, host...)
	if flags&srvKey != 0 {
		b = append(append(b, byte(len(key))), key...)
	}
	return b, nil
}

// unpackServer reads a packed server from the start of b and returns its
// address and how many bytes it took.
func unpackServer(b []byte) (string, int, error) {
	take := func(i int) ([]byte, int, bool) {
		if i >= len(b) || i+1+int(b[i]) > len(b) {
			return nil, 0, false
		}
		return b[i+1 : i+1+int(b[i])], i + 1 + int(b[i]), true
	}
	if len(b) < 2 {
		return "", 0, errBadCode
	}
	flags := b[0]
	hostB, n, ok := take(1)
	if !ok || len(hostB) == 0 {
		return "", 0, errBadCode
	}
	host := string(hostB)
	switch {
	case flags&srvDefault != 0:
		host = defaultName + host + workersDev
	case flags&srvWorkers != 0:
		host += workersDev
	}
	scheme := "https://"
	if flags&srvInsecure != 0 {
		scheme = "http://"
	}
	server := scheme + host
	if flags&srvKey != 0 {
		var key []byte
		if key, n, ok = take(n); !ok {
			return "", 0, errBadCode
		}
		k := string(key)
		if flags&srvKeyHex != 0 {
			k = hexString(key)
		}
		if !strings.Contains(host, "/") {
			server += "/"
		}
		server += "?key=" + url.QueryEscape(k)
	}
	if _, err := parseServer(server); err != nil {
		return "", 0, errBadCode
	}
	return server, n, nil
}

// packHex turns an even-length lowercase hex string into its bytes.
func packHex(s string) ([]byte, bool) {
	if len(s) == 0 || len(s)%2 != 0 {
		return nil, false
	}
	out := make([]byte, len(s)/2)
	for i := range out {
		hi, lo := hexVal(s[2*i]), hexVal(s[2*i+1])
		if hi < 0 || lo < 0 {
			return nil, false
		}
		out[i] = byte(hi<<4 | lo)
	}
	return out, true
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return -1
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 2*len(b))
	for i, c := range b {
		out[2*i], out[2*i+1] = digits[c>>4], digits[c&15]
	}
	return string(out)
}

package remote

import (
	"net/url"
	"strings"
)

// The signaling server travels inside the invitation, so guests need no
// settings of their own. Its key does not: only the host needs it, to open
// the room. The address is packed to keep codes short: https:// is implied
// and the usual Cloudflare address shrinks to the account's subdomain.
//
//	flags, len(host), host

const (
	srvInsecure  = 1 << 0 // http:// rather than https://
	srvWorkers   = 1 << 1 // host is <host>.workers.dev
	srvDefault   = 1 << 2 // host is emuhub-signal.<host>.workers.dev
	workersDev   = ".workers.dev"
	defaultName  = "emuhub-signal."
	maxServerLen = 255
)

// publicServer is the server's address without its key, as guests get it.
func publicServer(server string) string {
	u, err := url.Parse(strings.TrimSpace(server))
	if err != nil {
		return server
	}
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}

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
	if len(host) > maxServerLen {
		return nil, errServerTooLong
	}
	return append([]byte{flags, byte(len(host))}, host...), nil
}

// unpackServer reads a packed server from the start of b and returns its
// address and how many bytes it took.
func unpackServer(b []byte) (string, int, error) {
	if len(b) < 2 || int(b[1]) == 0 || 2+int(b[1]) > len(b) {
		return "", 0, errBadCode
	}
	flags, host := b[0], string(b[2:2+int(b[1])])
	if flags&^(srvInsecure|srvWorkers|srvDefault) != 0 {
		return "", 0, errBadCode
	}
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
	if _, err := parseServer(server); err != nil {
		return "", 0, errBadCode
	}
	return server, 2 + int(b[1]), nil
}

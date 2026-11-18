package remote

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/user"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Guests' names reach the host through Sunshine's pairing. Moonlight always
// introduces itself as "roth"; the guest's EmuHub rewrites that in the
// pairing requests, which travel as plain HTTP, and Sunshine shows the host
// the name in its list of pending pairings.

const (
	moonlightName = "roth"
	maxNameLen    = 24
)

var moonlightParam = []byte("devicename=" + moonlightName + "&")

// CleanName makes a player's name fit for the room: printable, single
// spaced and short. It may come back empty.
func CleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r) || r == unicode.ReplacementChar:
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxNameLen {
		s = strings.TrimSpace(string(r[:maxNameLen]))
	}
	return s
}

// DefaultName is the player's name until they choose one: their user name
// on this computer.
func DefaultName() string {
	if u, err := user.Current(); err == nil {
		if n := CleanName(u.Name); n != "" {
			return n
		}
		name := u.Username
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			name = name[i+1:] // DOMAIN\user on Windows
		}
		if n := CleanName(name); n != "" {
			return n
		}
	}
	if h, err := os.Hostname(); err == nil {
		return CleanName(h)
	}
	return ""
}

// guestName is the name Sunshine got for a pairing, or "" when the guest
// did not give one.
func guestName(name string) string {
	if name = CleanName(name); name == moonlightName {
		return ""
	}
	return name
}

// copyNaming copies Moonlight's HTTP requests from src to dst, putting the
// player's name where Moonlight says "roth".
func copyNaming(name string) func(dst io.Writer, src io.Reader) (int64, error) {
	param := []byte("devicename=" + strings.ReplaceAll(url.QueryEscape(name), "+", "%20") + "&")
	return func(dst io.Writer, src io.Reader) (int64, error) {
		// Request lines are long (the pairing one carries a certificate),
		// but the name comes right after the path.
		br := bufio.NewReaderSize(src, 64<<10)
		var n int64
		for {
			line, err := br.ReadSlice('\n')
			if bytes.HasPrefix(line, []byte("GET ")) {
				line = bytes.Replace(line, moonlightParam, param, 1)
			}
			if len(line) > 0 {
				w, werr := dst.Write(line)
				n += int64(w)
				if werr != nil {
					return n, werr
				}
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if errors.Is(err, io.EOF) {
				return n, nil
			}
			if err != nil {
				return n, err
			}
		}
	}
}

// pairRelay carries Moonlight's pairing with a host it reaches directly
// through loopback, so the guest's name can be added on the way. It listens
// on Sunshine's own HTTP and HTTPS port numbers, which Sunshine tells
// Moonlight to use.
type pairRelay struct {
	listeners []net.Listener
	wg        sync.WaitGroup
}

func startPairRelay(addr netip.Addr, port uint16, name string) (*pairRelay, error) {
	r := &pairRelay{}
	for _, p := range []uint16{port, port - 5} {
		l, err := net.Listen("tcp", hostPort(tunnelListen, p))
		if err != nil {
			r.Close()
			return nil, err
		}
		r.listeners = append(r.listeners, l)
		var copyIn func(io.Writer, io.Reader) (int64, error)
		if p == port {
			copyIn = copyNaming(name)
		}
		r.wg.Add(1)
		go r.serve(l, hostPort(addr, p), copyIn)
	}
	return r, nil
}

func (r *pairRelay) serve(l net.Listener, target string, copyIn func(io.Writer, io.Reader) (int64, error)) {
	defer r.wg.Done()
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var d net.Dialer
			u, err := d.DialContext(ctx, "tcp", target)
			if err != nil {
				c.Close()
				return
			}
			pipe(u.(*net.TCPConn), c.(*net.TCPConn), copyIn)
		}()
	}
}

// Close stops accepting; pairing connections still open finish on their own.
func (r *pairRelay) Close() {
	for _, l := range r.listeners {
		l.Close()
	}
	r.wg.Wait()
}

// relayHost is the address Moonlight pairs with through a pairRelay.
func relayHost(port uint16) string { return hostPort(tunnelListen, port) }

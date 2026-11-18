package remote

import (
	"bytes"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"  Omar   Mendoza ":             "Omar Mendoza",
		"Ana\x00\n\tLa\u0007 Grande":    "Ana La Grande",
		"roth":                          "roth",
		"🎮 Pepe":                        "🎮 Pepe",
		strings.Repeat("abcdefghij", 4): "abcdefghijabcdefghijabcd",
		"\x01\x02":                      "",
	} {
		if got := CleanName(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if guestName("roth") != "" || guestName(" Ana ") != "Ana" {
		t.Error("guestName")
	}
	if got := uniqueName("Ana", []string{"Ana", "Ana (2)"}); got != "Ana (3)" {
		t.Errorf("unique %q", got)
	}
	if SeatLabel(Seat{Kind: "guest", Guest: 2}) != "Invitado 2" || SeatLabel(Seat{Kind: "guest", Guest: 1, Name: "Ana"}) != "Ana" {
		t.Error("SeatLabel")
	}
}

func TestCopyNaming(t *testing.T) {
	cert := strings.Repeat("ab", 70_000) // longer than the read buffer
	in := "GET /pair?uniqueid=1&devicename=roth&updateState=1&phrase=getservercert&clientcert=" + cert + " HTTP/1.1\r\n" +
		"Host: 127.0.0.1\r\n\r\n" +
		"GET /serverinfo?uniqueid=1&devicename=roth&x=1 HTTP/1.1\r\nUser-Agent: devicename=roth&\r\n\r\n"
	var out bytes.Buffer
	if _, err := copyNaming("Ana María 🎮")(&out, strings.NewReader(in)); err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(in, "GET /pair?uniqueid=1&devicename=roth&", "GET /pair?uniqueid=1&devicename=Ana%20Mar%C3%ADa%20%F0%9F%8E%AE&")
	want = strings.Replace(want, "GET /serverinfo?uniqueid=1&devicename=roth&", "GET /serverinfo?uniqueid=1&devicename=Ana%20Mar%C3%ADa%20%F0%9F%8E%AE&", 1)
	if out.String() != want {
		t.Errorf("rewrite went wrong (%d bytes, want %d); headers must stay untouched", out.Len(), len(want))
	}
}

func TestPairRelay(t *testing.T) {
	if l, err := net.Listen("tcp", "127.0.0.2:0"); err != nil {
		t.Skip("127.0.0.2 is not available here")
	} else {
		l.Close()
	}
	tunnelListen = netip.MustParseAddr("127.0.0.2")
	t.Cleanup(func() { tunnelListen = tunnelTarget })

	// A pretend Sunshine on 127.0.0.1 that answers with the request line.
	const port = 53489
	for _, p := range []uint16{port, port - 5} {
		l, err := net.Listen("tcp", hostPort(tunnelTarget, p))
		if err != nil {
			t.Skipf("port %d busy", p)
		}
		t.Cleanup(func() { l.Close() })
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				go func() {
					defer c.Close()
					b := make([]byte, 4096)
					n, _ := c.Read(b)
					c.Write(b[:n])
				}()
			}
		}()
	}
	r, err := startPairRelay(tunnelTarget, port, "Ana")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ask := func(p uint16, req string) string {
		c, err := net.Dial("tcp", hostPort(tunnelListen, p))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		c.Write([]byte(req))
		c.(*net.TCPConn).CloseWrite()
		b, _ := io.ReadAll(c)
		return string(b)
	}
	if got := ask(port, "GET /pair?devicename=roth&a=1 HTTP/1.1\r\n\r\n"); !strings.Contains(got, "devicename=Ana&") {
		t.Errorf("http: %q", got)
	}
	// HTTPS is encrypted; it passes untouched.
	if got := ask(port-5, "GET /pair?devicename=roth&a=1 HTTP/1.1\r\n\r\n"); !strings.Contains(got, "devicename=roth&") {
		t.Errorf("https: %q", got)
	}
}

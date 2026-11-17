package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/ice/v4"
)

func TestReassembler(t *testing.T) {
	var f fragmenter
	var got [][]byte
	sendAll := func(p []byte) [][]byte {
		var out [][]byte
		count := max(1, (len(p)+fragData-1)/fragData)
		id := uint16(f.next.Add(1))
		for i := range count {
			chunk := p[i*fragData : min((i+1)*fragData, len(p))]
			d := make([]byte, fragHeader, fragHeader+len(chunk))
			d[0], d[1] = 0xbb, 0x9a
			d[2], d[3] = byte(id>>8), byte(id)
			d[4], d[5] = byte(i), byte(count)
			out = append(out, append(d, chunk...))
		}
		return out
	}
	var r reassembler
	big := bytes.Repeat([]byte("emuhub"), 500) // three pieces
	small := []byte("hola")
	a, b := sendAll(big), sendAll(small)
	// Out of order, interleaved and with a duplicate.
	for _, d := range [][]byte{a[2], b[0], a[0], a[0], a[1]} {
		if port, data, ok := r.add(d); ok {
			if port != 0xbb9a {
				t.Errorf("port %x", port)
			}
			got = append(got, data)
		}
	}
	if len(got) != 2 || !bytes.Equal(got[0], small) || !bytes.Equal(got[1], big) {
		t.Fatalf("got %d packets", len(got))
	}
	// Lost pieces do not pile up.
	for range 3 * maxPartials {
		r.add(sendAll(big)[0])
	}
	if len(r.parts) > maxPartials || len(r.order) > maxPartials {
		t.Errorf("%d partial packets kept", len(r.parts))
	}
}

// fakeSignal mirrors the Worker in signal/ closely enough for tests.
func fakeSignal(t *testing.T, key string) string {
	type roomConns struct {
		host   *websocket.Conn
		guests map[string]*websocket.Conn
	}
	var (
		mu    sync.Mutex
		rooms = map[string]*roomConns{}
		next  int
	)
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if key != "" && r.URL.Query().Get("role") == "host" && r.URL.Query().Get("key") != key {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		room := strings.TrimPrefix(r.URL.Path, "/v1/rooms/")
		role := r.URL.Query().Get("role")
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		send := func(c *websocket.Conn, b []byte) {
			wmu.Lock()
			defer wmu.Unlock()
			c.WriteMessage(websocket.TextMessage, b)
		}
		mu.Lock()
		rm := rooms[room]
		if rm == nil {
			rm = &roomConns{guests: map[string]*websocket.Conn{}}
			rooms[room] = rm
		}
		id := ""
		if role == "host" {
			rm.host = ws
		} else {
			next++
			id = string(rune('a' + next))
			rm.guests[id] = ws
			if rm.host == nil {
				mu.Unlock()
				send(ws, []byte(`{"type":"error","reason":"nohost"}`))
				ws.Close()
				return
			}
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			if role == "host" && rm.host == ws {
				rm.host = nil
			}
			delete(rm.guests, id)
			mu.Unlock()
			ws.Close()
		}()
		for {
			_, b, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if string(b) == "ping" {
				send(ws, []byte("pong"))
				continue
			}
			var m map[string]any
			if json.Unmarshal(b, &m) != nil {
				continue
			}
			mu.Lock()
			var to *websocket.Conn
			if role == "host" {
				to, _ = rm.guests[m["to"].(string)]
			} else {
				m["from"] = id
				b, _ = json.Marshal(m)
				to = rm.host
			}
			mu.Unlock()
			if to != nil {
				send(to, b)
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// echoSunshine stands in for Sunshine: every port sends back what it gets.
func echoSunshine(t *testing.T, base uint16) {
	for _, p := range tcpPorts(base) {
		l, err := net.Listen("tcp", hostPort(tunnelTarget, p))
		if err != nil {
			t.Skipf("port %d busy: %v", p, err)
		}
		t.Cleanup(func() { l.Close() })
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				go func() {
					io.Copy(c, c)
					c.Close()
				}()
			}
		}()
	}
	for _, p := range udpPorts(base) {
		c, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(netAddrPort(tunnelTarget, p)))
		if err != nil {
			t.Skipf("port %d busy: %v", p, err)
		}
		t.Cleanup(func() { c.Close() })
		go func() {
			buf := make([]byte, 65536)
			for {
				n, addr, err := c.ReadFromUDP(buf)
				if err != nil {
					return
				}
				c.WriteToUDP(buf[:n], addr)
			}
		}()
	}
}

func TestTunnel(t *testing.T) {
	if l, err := net.Listen("tcp", "127.0.0.2:0"); err != nil {
		t.Skip("127.0.0.2 is not available here")
	} else {
		l.Close()
	}
	tunnelListen = netip.MustParseAddr("127.0.0.2")
	oldSTUN := stunServers
	stunServers = nil
	extraICEOptions = []ice.AgentOption{ice.WithIncludeLoopback()}
	t.Cleanup(func() {
		tunnelListen = tunnelTarget
		stunServers = oldSTUN
		extraICEOptions = nil
	})
	// The host's address carries the key; guests get it without.
	server := os.Getenv("EMUHUB_SIGNAL_URL")
	if server == "" {
		server = fakeSignal(t, "k") + "/?key=k"
	}
	guestServer := publicServer(server)

	const base = 52489
	echoSunshine(t, base)
	secret := make([]byte, secretLen)
	rand.Read(secret)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// Nobody is hosting this room yet.
	if _, err := dialTunnel(ctx, guestServer, Invite{Port: base, Secret: secret}, "Ana"); !errors.Is(err, errNoTunnelHost) {
		t.Fatalf("without host: %v", err)
	}

	host, err := startTunnelHost(ctx, server, secret, base)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()

	var g *tunnelGuest
	for range 20 {
		g, err = dialTunnel(ctx, guestServer, Invite{Port: base, Secret: secret}, "Ana")
		if !errors.Is(err, errNoTunnelHost) {
			break
		}
		time.Sleep(200 * time.Millisecond) // the host is still joining the room
	}
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	// TCP, including passing on the end of the stream.
	for _, p := range tcpPorts(base) {
		c, err := net.Dial("tcp", hostPort(tunnelListen, p))
		if err != nil {
			t.Fatal(err)
		}
		msg := bytes.Repeat([]byte{byte(p)}, 100_000)
		go func() {
			c.Write(msg)
			c.(*net.TCPConn).CloseWrite()
		}()
		c.SetReadDeadline(time.Now().Add(10 * time.Second))
		got, err := io.ReadAll(c)
		c.Close()
		if err != nil || !bytes.Equal(got, msg) {
			t.Fatalf("tcp %d: %d bytes back, %v", p, len(got), err)
		}
	}

	// Pairing requests on the HTTP port carry the guest's name.
	{
		c, err := net.Dial("tcp", hostPort(tunnelListen, base))
		if err != nil {
			t.Fatal(err)
		}
		c.Write([]byte("GET /pair?devicename=roth&x=1 HTTP/1.1\r\n\r\n"))
		c.(*net.TCPConn).CloseWrite()
		c.SetReadDeadline(time.Now().Add(10 * time.Second))
		got, _ := io.ReadAll(c)
		c.Close()
		if !bytes.Contains(got, []byte("devicename=Ana&")) {
			t.Errorf("pairing request reached the host as %q", got)
		}
	}

	// The web UI is not forwarded.
	if c, err := net.DialTimeout("tcp", hostPort(tunnelListen, base+1), time.Second); err == nil {
		c.Close()
		t.Error("web UI port is open on the guest")
	}

	// UDP, with packets that need splitting.
	for _, p := range udpPorts(base) {
		c, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(netAddrPort(tunnelListen, p)))
		if err != nil {
			t.Fatal(err)
		}
		msg := bytes.Repeat([]byte{byte(p)}, 3000)
		buf := make([]byte, 65536)
		ok := false
		for range 10 {
			c.Write(msg)
			c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, err := c.Read(buf)
			if err == nil && bytes.Equal(buf[:n], msg) {
				ok = true
				break
			}
		}
		c.Close()
		if !ok {
			t.Fatalf("udp %d: no echo", p)
		}
	}

	// A guest with the wrong secret lands in another room.
	other := append([]byte(nil), secret...)
	other[0] ^= 1
	if _, err := dialTunnel(ctx, guestServer, Invite{Port: base + 100, Secret: other}, ""); !errors.Is(err, errNoTunnelHost) {
		t.Errorf("wrong secret: %v", err)
	}

	// Closing the room ends the guest's tunnel.
	host.Close()
	select {
	case <-g.Done():
	case <-time.After(10 * time.Second):
		t.Error("guest tunnel still open after the host left")
	}
}

func TestServerKey(t *testing.T) {
	server := fakeSignal(t, "s3creto")
	secret := []byte("12345678")
	ctx := context.Background()
	if _, err := startTunnelHost(ctx, server, secret, 52489); !errors.Is(err, errBadServerKey) {
		t.Errorf("without key: %v", err)
	}
	if _, err := startTunnelHost(ctx, server+"/?key=otra", secret, 52489); !errors.Is(err, errBadServerKey) {
		t.Errorf("wrong key: %v", err)
	}
	h, err := startTunnelHost(ctx, server+"/?key=s3creto", secret, 52489)
	if err != nil {
		t.Fatalf("right key: %v", err)
	}
	defer h.Close()
	// Guests need no key, only the room's name.
	sc, err := dialSignal(ctx, server, secret, "guest")
	if err != nil {
		t.Fatalf("guest without key: %v", err)
	}
	sc.Close()
}

func TestRoomURL(t *testing.T) {
	secret := []byte("12345678")
	for in, want := range map[string]string{
		"https://a.workers.dev":                "wss://a.workers.dev/v1/rooms/" + roomKey(secret) + "?role=host",
		" https://a.workers.dev/?key=x%2By#f ": "wss://a.workers.dev/v1/rooms/" + roomKey(secret) + "?key=x%2By&role=host",
	} {
		got, err := roomURL(in, secret, "host")
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	// Guests never send the key.
	if got, _ := roomURL("https://a.workers.dev/?key=x", secret, "guest"); strings.Contains(got, "key") {
		t.Errorf("guest url %q", got)
	}
	for in, want := range map[string]string{
		"http://127.0.0.1:8787/": "ws://127.0.0.1:8787/v1/rooms/" + roomKey(secret) + "?role=host",
	} {
		got, err := roomURL(in, secret, "host")
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"emuhub-signal.workers.dev", "ftp://a.dev", "https://"} {
		if CheckServer(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if CheckServer("  ") != nil {
		t.Error("empty rejected")
	}
}

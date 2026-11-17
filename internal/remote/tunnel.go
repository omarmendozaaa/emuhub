package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/stun/v4"
	"github.com/quic-go/quic-go"
)

// The tunnel carries a room between players on different networks, without
// opening ports on any router or firewall.
//
// Both sides meet on the signaling server (see signal.go), exchange the
// addresses their NATs give them (ICE, with public STUN servers) and send UDP
// to each other at the same time, which opens a path through both NATs. A QUIC
// connection over that path then carries Sunshine's ports: TCP ports as
// streams and UDP ports as datagrams. On the guest's machine EmuHub listens
// on the same port numbers on loopback, so Moonlight connects to 127.0.0.1
// and neither Sunshine nor Moonlight know about the tunnel.
//
// Two NATs that both change port per destination (symmetric NAT, common on
// mobile data) cannot be traversed this way; the guest then gets an error.

const (
	tunnelALPN = "emuhub-tunnel/1"
	// fragData keeps every datagram below the smallest packet QUIC sends
	// before it has discovered the path's MTU. Moonlight's video packets are
	// larger on what it thinks is a local network, so they travel in two.
	fragData   = 1150
	fragHeader = 6
	// maxTunnels bounds the guests a host serves at once, counting those
	// still connecting.
	maxTunnels = MaxGuests * 2
)

func init() {
	// QUIC runs over ICE's socket, whose buffers belong to pion; quic-go
	// would otherwise log that it cannot resize them.
	if os.Getenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING") == "" {
		os.Setenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING", "true")
	}
}

// stunServers tell each side its address as seen from the internet.
var stunServers = []string{"stun:stun.l.google.com:19302", "stun:stun.cloudflare.com:3478"}

// Loopback addresses on each end. Tests move the guest to 127.0.0.2 so both
// ends fit on one machine.
var (
	tunnelTarget = netip.MustParseAddr("127.0.0.1")
	tunnelListen = netip.MustParseAddr("127.0.0.1")
	// extraICEOptions lets tests connect two agents on loopback without STUN.
	extraICEOptions []ice.AgentOption
)

// tcpPorts are Sunshine's TCP ports that Moonlight uses: HTTPS, HTTP and
// RTSP. The web UI (base+1) stays off the tunnel.
func tcpPorts(base uint16) []uint16 { return []uint16{base - 5, base, base + 21} }

// udpPorts are Sunshine's video, control and audio ports.
func udpPorts(base uint16) []uint16 { return []uint16{base + 9, base + 10, base + 11} }

// roomKey is the room's name on the signaling server. It is a hash, so the
// server never learns the secret that authenticates the tunnel.
func roomKey(secret []byte) string {
	sum := sha256.Sum256(append([]byte("emuhub-room\x00"), secret...))
	return hex.EncodeToString(sum[:16])
}

func mac(secret []byte, parts ...string) []byte {
	m := hmac.New(sha256.New, secret)
	for _, p := range parts {
		m.Write([]byte(p))
		m.Write([]byte{0})
	}
	return m.Sum(nil)
}

// iceInfo is one side's ICE credentials and candidates.
type iceInfo struct {
	Ufrag      string   `json:"ufrag"`
	Pwd        string   `json:"pwd"`
	Candidates []string `json:"candidates"`
}

func newAgent() (*ice.Agent, error) {
	var urls []*stun.URI
	for _, s := range stunServers {
		u, err := stun.ParseURI(s)
		if err != nil {
			return nil, err
		}
		urls = append(urls, u)
	}
	opts := []ice.AgentOption{
		ice.WithUrls(urls),
		ice.WithNetworkTypes([]ice.NetworkType{ice.NetworkTypeUDP4, ice.NetworkTypeUDP6}),
		ice.WithCandidateTypes([]ice.CandidateType{ice.CandidateTypeHost, ice.CandidateTypeServerReflexive}),
		ice.WithMulticastDNSMode(ice.MulticastDNSModeDisabled),
		ice.WithInterfaceFilter(func(name string) bool { return !isVirtual(name) }),
	}
	return ice.NewAgentWithOptions(append(opts, extraICEOptions...)...)
}

// gather collects every local candidate. Candidates are sent in one message
// rather than trickled, which costs a second at most.
func gather(ctx context.Context, agent *ice.Agent) (iceInfo, error) {
	var (
		mu    sync.Mutex
		cands []string
		done  = make(chan struct{})
		once  sync.Once
	)
	if err := agent.OnCandidate(func(c ice.Candidate) {
		if c == nil {
			once.Do(func() { close(done) })
			return
		}
		mu.Lock()
		cands = append(cands, c.Marshal())
		mu.Unlock()
	}); err != nil {
		return iceInfo{}, err
	}
	if err := agent.GatherCandidates(); err != nil {
		return iceInfo{}, err
	}
	select {
	case <-done:
	case <-ctx.Done():
		return iceInfo{}, ctx.Err()
	case <-time.After(8 * time.Second):
		// Use whatever arrived; a slow STUN server is not fatal.
	}
	ufrag, pwd, err := agent.GetLocalUserCredentials()
	if err != nil {
		return iceInfo{}, err
	}
	mu.Lock()
	defer mu.Unlock()
	if len(cands) == 0 {
		return iceInfo{}, errors.New("no se encontró ninguna red para el túnel")
	}
	return iceInfo{Ufrag: ufrag, Pwd: pwd, Candidates: append([]string(nil), cands...)}, nil
}

func addRemote(agent *ice.Agent, info *iceInfo) error {
	added := 0
	for _, s := range info.Candidates {
		c, err := ice.UnmarshalCandidate(s)
		if err != nil {
			continue
		}
		if agent.AddRemoteCandidate(c) == nil {
			added++
		}
	}
	if added == 0 {
		return errors.New("el otro jugador no envió direcciones válidas")
	}
	return nil
}

// tunnelAddr stands for the peer in QUIC's eyes. It never changes, even when
// ICE switches to another candidate pair, so QUIC does not see a migration.
var tunnelAddr = &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1}

// icePacketConn lets QUIC run over an ICE connection.
type icePacketConn struct{ *ice.Conn }

func (c icePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := c.Read(p)
	return n, tunnelAddr, err
}

func (c icePacketConn) WriteTo(p []byte, _ net.Addr) (int, error) { return c.Write(p) }

func quicConfig() *quic.Config {
	return &quic.Config{
		EnableDatagrams:      true,
		KeepAlivePeriod:      5 * time.Second,
		MaxIdleTimeout:       20 * time.Second,
		HandshakeIdleTimeout: 10 * time.Second,
	}
}

// selfSigned makes the host's certificate for one session. The guest trusts
// it by fingerprint, which reaches it signed with the room secret.
func selfSigned() (tls.Certificate, string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(48 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	sum := sha256.Sum256(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, hex.EncodeToString(sum[:]), nil
}

// guestProof is what the guest sends first on the connection to show it
// knows the room secret. It is bound to this TLS session, so it cannot be
// replayed on another one.
func guestProof(qc *quic.Conn, secret []byte) ([]byte, error) {
	cs := qc.ConnectionState().TLS
	ekm, err := cs.ExportKeyingMaterial("emuhub tunnel", nil, 32)
	if err != nil {
		return nil, err
	}
	return mac(secret, "guest", string(ekm)), nil
}

// ---- Datagrams ----

// Each datagram is a UDP packet, or a piece of one, for one of Sunshine's
// ports: port, packet id, piece index and piece count, then the data.

type fragmenter struct{ next atomic.Uint32 }

func (f *fragmenter) send(qc *quic.Conn, port uint16, p []byte) {
	count := (len(p) + fragData - 1) / fragData
	if count == 0 {
		count = 1
	}
	if count > 255 {
		return
	}
	id := uint16(f.next.Add(1))
	buf := make([]byte, fragHeader+fragData)
	for i := range count {
		chunk := p[i*fragData : min((i+1)*fragData, len(p))]
		binary.BigEndian.PutUint16(buf[0:], port)
		binary.BigEndian.PutUint16(buf[2:], id)
		buf[4], buf[5] = byte(i), byte(count)
		n := copy(buf[fragHeader:], chunk)
		// Lost or refused datagrams are like lost UDP packets: Moonlight
		// copes with those.
		_ = qc.SendDatagram(buf[:fragHeader+n])
	}
}

type partial struct {
	got    int
	pieces [][]byte
}

// reassembler puts packets back together. It keeps only the latest few
// incomplete ones; a piece that never comes means the packet was lost.
type reassembler struct {
	parts map[uint32]*partial
	order []uint32
}

const maxPartials = 64

func (r *reassembler) add(d []byte) (uint16, []byte, bool) {
	if len(d) < fragHeader {
		return 0, nil, false
	}
	port := binary.BigEndian.Uint16(d[0:])
	id := binary.BigEndian.Uint16(d[2:])
	idx, count := int(d[4]), int(d[5])
	data := d[fragHeader:]
	if count == 1 && idx == 0 {
		return port, data, true
	}
	if count == 0 || idx >= count {
		return 0, nil, false
	}
	if r.parts == nil {
		r.parts = map[uint32]*partial{}
	}
	key := uint32(port)<<16 | uint32(id)
	p := r.parts[key]
	if p == nil || len(p.pieces) != count {
		if len(r.order) >= maxPartials {
			delete(r.parts, r.order[0])
			r.order = r.order[1:]
		}
		p = &partial{pieces: make([][]byte, count)}
		r.parts[key] = p
		r.order = append(r.order, key)
	}
	if p.pieces[idx] != nil {
		return 0, nil, false
	}
	p.pieces[idx] = append([]byte(nil), data...)
	p.got++
	if p.got < count {
		return 0, nil, false
	}
	delete(r.parts, key)
	for i, k := range r.order {
		if k == key {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	var out []byte
	for _, piece := range p.pieces {
		out = append(out, piece...)
	}
	return port, out, true
}

// ---- Streams ----

// halfStream makes a QUIC stream close like a TCP socket: Close stops both
// directions, CloseWrite only ours.
type halfStream struct{ *quic.Stream }

func (s halfStream) CloseWrite() error { return s.Stream.Close() }

func (s halfStream) Close() error {
	s.Stream.CancelRead(0)
	return s.Stream.Close()
}

type halfCloser interface {
	io.ReadWriteCloser
	CloseWrite() error
}

// pipe copies both ways. Each direction passes its end of stream on, as TCP
// does, and both ends close once both directions are done. copyIn, when not
// nil, copies from b to a in place of io.Copy.
func pipe(a, b halfCloser, copyIn func(io.Writer, io.Reader) (int64, error)) {
	done := make(chan struct{}, 2)
	cp := func(dst, src halfCloser, copy func(io.Writer, io.Reader) (int64, error)) {
		copy(dst, src)
		dst.CloseWrite()
		done <- struct{}{}
	}
	if copyIn == nil {
		copyIn = io.Copy
	}
	go cp(a, b, copyIn)
	go cp(b, a, io.Copy)
	<-done
	<-done
	a.Close()
	b.Close()
}

func writePortHeader(w io.Writer, port uint16) error {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], port)
	_, err := w.Write(b[:])
	return err
}

func readPortHeader(r io.Reader) (uint16, error) {
	var b [2]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b[:]), nil
}

func contains(ports []uint16, p uint16) bool {
	for _, q := range ports {
		if q == p {
			return true
		}
	}
	return false
}

func netAddrPort(addr netip.Addr, port uint16) netip.AddrPort {
	return netip.AddrPortFrom(addr, port)
}

func hostPort(addr netip.Addr, port uint16) string { return netAddrPort(addr, port).String() }

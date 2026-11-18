package remote

import (
	"context"
	"crypto/hmac"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

// tunnelHost waits in the room on the signaling server and serves a tunnel
// to each guest who arrives there.
type tunnelHost struct {
	server string
	secret []byte
	base   uint16
	cert   tls.Certificate
	fp     string
	ctx    context.Context
	cancel context.CancelFunc
	active atomic.Int32
	wg     sync.WaitGroup
}

// startTunnelHost joins the room on the server. The first connection is
// made here, so a wrong address or key shows up when the room opens; later
// drops are retried in the background.
func startTunnelHost(ctx context.Context, server string, secret []byte, base uint16) (*tunnelHost, error) {
	cert, fp, err := selfSigned()
	if err != nil {
		return nil, err
	}
	dialCtx, cancelDial := context.WithTimeout(ctx, 10*time.Second)
	sc, err := dialSignal(dialCtx, server, secret, "host")
	cancelDial()
	if err != nil {
		return nil, err
	}
	loopCtx, cancel := context.WithCancel(context.Background())
	t := &tunnelHost{server: server, secret: secret, base: base, cert: cert, fp: fp, ctx: loopCtx, cancel: cancel}
	t.wg.Add(1)
	go t.signalLoop(sc)
	return t, nil
}

// Close leaves the room and ends every tunnel.
func (t *tunnelHost) Close() {
	t.cancel()
	t.wg.Wait()
}

// signalLoop stays connected to the room for as long as it is open.
func (t *tunnelHost) signalLoop(sc *signalConn) {
	defer t.wg.Done()
	backoff := time.Second
	for {
		start := time.Now()
		err := t.serveSignal(sc)
		sc = nil
		if t.ctx.Err() != nil {
			return
		}
		log.Printf("tunnel: signaling: %v", err)
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		select {
		case <-t.ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// serveSignal listens in the room until the connection drops. It dials
// unless handed a connection already made.
func (t *tunnelHost) serveSignal(sc *signalConn) error {
	if sc == nil {
		var err error
		if sc, err = dialSignal(t.ctx, t.server, t.secret, "host"); err != nil {
			return err
		}
	}
	defer sc.Close()
	stop := context.AfterFunc(t.ctx, func() { sc.Close() })
	defer stop()
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for range tick.C {
			if sc.ping() != nil {
				return
			}
		}
	}()
	for {
		m, err := sc.read()
		if err != nil {
			return err
		}
		if m.Type != "offer" || m.From == "" || m.ICE == nil {
			continue
		}
		if t.active.Load() >= maxTunnels {
			sc.send(signalMsg{Type: "error", To: m.From, Reason: "full"})
			continue
		}
		t.active.Add(1)
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			defer t.active.Add(-1)
			if err := t.serveGuest(sc, m); err != nil && t.ctx.Err() == nil {
				log.Printf("tunnel: guest %s: %v", m.From, err)
			}
		}()
	}
}

// serveGuest answers one guest's offer, waits for it to connect and then
// forwards its traffic to Sunshine until either side leaves.
func (t *tunnelHost) serveGuest(sc *signalConn, offer signalMsg) error {
	setup, cancel := context.WithTimeout(t.ctx, 45*time.Second)
	defer cancel()

	agent, err := newAgent()
	if err != nil {
		return err
	}
	defer agent.Close()
	info, err := gather(setup, agent)
	if err != nil {
		return err
	}
	if err := addRemote(agent, offer.ICE); err != nil {
		return err
	}
	if err := sc.send(signalMsg{
		Type: "answer",
		To:   offer.From,
		ICE:  &info,
		FP:   t.fp,
		MAC:  hex.EncodeToString(mac(t.secret, "answer", t.fp, offer.ICE.Ufrag, info.Ufrag)),
	}); err != nil {
		return err
	}
	conn, err := agent.Accept(setup, offer.ICE.Ufrag, offer.ICE.Pwd)
	if err != nil {
		return err
	}

	tr := &quic.Transport{Conn: icePacketConn{conn}}
	defer tr.Close()
	ln, err := tr.Listen(&tls.Config{
		Certificates: []tls.Certificate{t.cert},
		NextProtos:   []string{tunnelALPN},
		MinVersion:   tls.VersionTLS13,
	}, quicConfig())
	if err != nil {
		return err
	}
	qc, err := ln.Accept(setup)
	ln.Close()
	if err != nil {
		return err
	}
	defer qc.CloseWithError(0, "")
	if err := checkGuest(setup, qc, t.secret); err != nil {
		qc.CloseWithError(1, "unauthorized")
		return err
	}
	cancel()

	s := &hostSession{qc: qc, base: t.base, udp: map[uint16]*net.UDPConn{}}
	s.run(t.ctx)
	return nil
}

// checkGuest reads the guest's proof that it knows the room secret, which
// must come before anything is forwarded.
func checkGuest(ctx context.Context, qc *quic.Conn, secret []byte) error {
	st, err := qc.AcceptStream(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	st.SetDeadline(time.Now().Add(10 * time.Second))
	want, err := guestProof(qc, secret)
	if err != nil {
		return err
	}
	port, err := readPortHeader(st)
	if err != nil {
		return err
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(st, got); err != nil {
		return err
	}
	if port != 0 || !hmac.Equal(got, want) {
		return errors.New("el invitado no conoce el secreto de la sala")
	}
	_, err = st.Write([]byte{1})
	return err
}

// hostSession forwards one guest's tunnel to Sunshine on loopback.
type hostSession struct {
	qc   *quic.Conn
	base uint16
	frag fragmenter

	mu  sync.Mutex
	udp map[uint16]*net.UDPConn
}

func (s *hostSession) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-ctx.Done():
		case <-s.qc.Context().Done():
		}
		cancel()
		s.qc.CloseWithError(0, "")
	}()
	go s.datagrams(ctx)
	for {
		st, err := s.qc.AcceptStream(ctx)
		if err != nil {
			break
		}
		go s.stream(st)
	}
	s.mu.Lock()
	for _, c := range s.udp {
		c.Close()
	}
	s.mu.Unlock()
}

func (s *hostSession) stream(st *quic.Stream) {
	hs := halfStream{st}
	st.SetReadDeadline(time.Now().Add(10 * time.Second))
	port, err := readPortHeader(st)
	st.SetReadDeadline(time.Time{})
	if err != nil || !contains(tcpPorts(s.base), port) {
		hs.Close()
		return
	}
	c, err := net.DialTimeout("tcp", hostPort(tunnelTarget, port), 5*time.Second)
	if err != nil {
		hs.Close()
		return
	}
	pipe(hs, c.(*net.TCPConn), nil)
}

func (s *hostSession) datagrams(ctx context.Context) {
	var r reassembler
	for {
		d, err := s.qc.ReceiveDatagram(ctx)
		if err != nil {
			return
		}
		port, data, ok := r.add(d)
		if !ok || !contains(udpPorts(s.base), port) {
			continue
		}
		if c := s.udpConn(port); c != nil {
			c.Write(data)
		}
	}
}

// udpConn is the socket that stands for the guest on one of Sunshine's UDP
// ports. Sunshine answers to it, and its replies go back through the tunnel.
func (s *hostSession) udpConn(port uint16) *net.UDPConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.udp[port]; ok {
		return c
	}
	c, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(netAddrPort(tunnelTarget, port)))
	if err != nil {
		return nil
	}
	s.udp[port] = c
	go func() {
		buf := make([]byte, 65536)
		for {
			n, err := c.Read(buf)
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if err != nil {
				// Refused while Sunshine is not listening yet; keep going.
				time.Sleep(10 * time.Millisecond)
				continue
			}
			s.frag.send(s.qc, port, buf[:n])
		}
	}()
	return c
}

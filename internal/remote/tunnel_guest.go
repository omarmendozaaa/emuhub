package remote

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/quic-go/quic-go"
)

var (
	errNoTunnelHost = errors.New("la sala no está esperando invitados por internet: puede que ya se haya cerrado. Pide al anfitrión la invitación que aparece ahora en su sala")
	errRoomFull     = errors.New("la sala está completa")
	errNoPath       = errors.New("no se pudo abrir un camino hasta el anfitrión: vuestras dos redes bloquean las conexiones entre equipos (pasa a menudo con datos móviles). Probad desde otra red, o que el anfitrión abra los puertos de su router")
)

// tunnelGuest is the guest's end of a tunnel: listeners on loopback that
// Moonlight connects to, and the QUIC connection to the host.
type tunnelGuest struct {
	agent *ice.Agent
	tr    *quic.Transport
	qc    *quic.Conn
	tcp   []net.Listener
	udp   map[uint16]*net.UDPConn
	frag  fragmenter

	mu    sync.Mutex
	peers map[uint16]*net.UDPAddr // where Moonlight listens, per port

	closeOnce sync.Once
}

// dialTunnel opens a tunnel to the host of the room. Once it returns,
// Moonlight reaches the host at tunnelListen on the room's port.
func dialTunnel(ctx context.Context, server string, in Invite) (*tunnelGuest, error) {
	g := &tunnelGuest{udp: map[uint16]*net.UDPConn{}, peers: map[uint16]*net.UDPAddr{}}
	ok := false
	defer func() {
		if !ok {
			g.Close()
		}
	}()

	// Listen first: there is no point in reaching the host if Moonlight then
	// cannot reach us.
	for _, p := range tcpPorts(in.Port) {
		l, err := net.Listen("tcp", hostPort(tunnelListen, p))
		if err != nil {
			return nil, busyPort(p, err)
		}
		g.tcp = append(g.tcp, l)
	}
	for _, p := range udpPorts(in.Port) {
		c, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(netAddrPort(tunnelListen, p)))
		if err != nil {
			return nil, busyPort(p, err)
		}
		g.udp[p] = c
	}

	setup, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var err error
	if g.agent, err = newAgent(); err != nil {
		return nil, err
	}
	info, err := gather(setup, g.agent)
	if err != nil {
		return nil, err
	}
	answer, err := exchange(setup, server, in.Secret, info)
	if err != nil {
		return nil, err
	}
	if err := addRemote(g.agent, answer.ICE); err != nil {
		return nil, err
	}
	conn, err := g.agent.Dial(setup, answer.ICE.Ufrag, answer.ICE.Pwd)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errNoPath
	}

	g.tr = &quic.Transport{Conn: icePacketConn{conn}}
	g.qc, err = g.tr.Dial(setup, tunnelAddr, &tls.Config{
		// The host's certificate is self-signed; it is trusted by the
		// fingerprint that came signed with the room secret.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("sin certificado")
			}
			sum := sha256.Sum256(raw[0])
			if hex.EncodeToString(sum[:]) != answer.FP {
				return errors.New("el certificado del anfitrión no coincide")
			}
			return nil
		},
		NextProtos: []string{tunnelALPN},
		MinVersion: tls.VersionTLS13,
	}, quicConfig())
	if err != nil {
		return nil, fmt.Errorf("no se pudo conectar con el anfitrión por el túnel: %w", err)
	}
	if err := proveGuest(setup, g.qc, in.Secret); err != nil {
		return nil, fmt.Errorf("el anfitrión rechazó el túnel: %w", err)
	}

	for i, l := range g.tcp {
		go g.acceptTCP(l, tcpPorts(in.Port)[i])
	}
	for p, c := range g.udp {
		go g.readUDP(c, p)
	}
	go g.datagrams()
	go func() {
		<-g.qc.Context().Done()
		g.Close()
	}()
	ok = true
	return g, nil
}

func busyPort(p uint16, err error) error {
	return fmt.Errorf("no se puede usar el puerto %d de este equipo para la sala (¿tienes Sunshine abierto?): %w", p, err)
}

// exchange meets the host on the signaling server: it sends this guest's
// ICE details and waits for the host's, checking they really come from it.
func exchange(ctx context.Context, server string, secret []byte, info iceInfo) (signalMsg, error) {
	sc, err := dialSignal(ctx, server, secret, "guest")
	if errors.Is(err, errBadServerKey) || errors.Is(err, errNotAServer) {
		return signalMsg{}, err
	}
	if err != nil {
		return signalMsg{}, fmt.Errorf("no se pudo contactar con el servidor de salas: %w", err)
	}
	defer sc.Close()
	stop := context.AfterFunc(ctx, func() { sc.Close() })
	defer stop()
	if err := sc.send(signalMsg{Type: "offer", ICE: &info}); err != nil {
		return signalMsg{}, err
	}
	for {
		m, err := sc.read()
		if err != nil {
			if ctx.Err() != nil {
				return signalMsg{}, errors.New("el anfitrión no respondió a tiempo")
			}
			return signalMsg{}, fmt.Errorf("se perdió la conexión con el servidor de salas: %w", err)
		}
		switch m.Type {
		case "error":
			if m.Reason == "full" {
				return signalMsg{}, errRoomFull
			}
			return signalMsg{}, errNoTunnelHost
		case "answer":
			if m.ICE == nil {
				continue
			}
			got, _ := hex.DecodeString(m.MAC)
			if !hmac.Equal(got, mac(secret, "answer", m.FP, info.Ufrag, m.ICE.Ufrag)) {
				// Not from someone who knows the secret; keep waiting.
				continue
			}
			return m, nil
		}
	}
}

// proveGuest sends the proof that this guest knows the room secret and
// waits for the host to accept it.
func proveGuest(ctx context.Context, qc *quic.Conn, secret []byte) error {
	proof, err := guestProof(qc, secret)
	if err != nil {
		return err
	}
	st, err := qc.OpenStreamSync(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	st.SetDeadline(time.Now().Add(10 * time.Second))
	if err := writePortHeader(st, 0); err != nil {
		return err
	}
	if _, err := st.Write(proof); err != nil {
		return err
	}
	var ack [1]byte
	if _, err := io.ReadFull(st, ack[:]); err != nil {
		return err
	}
	return nil
}

func (g *tunnelGuest) acceptTCP(l net.Listener, port uint16) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			st, err := g.qc.OpenStreamSync(g.qc.Context())
			if err != nil {
				c.Close()
				return
			}
			if writePortHeader(st, port) != nil {
				halfStream{st}.Close()
				c.Close()
				return
			}
			pipe(halfStream{st}, c.(*net.TCPConn))
		}()
	}
}

func (g *tunnelGuest) readUDP(c *net.UDPConn, port uint16) {
	buf := make([]byte, 65536)
	for {
		n, addr, err := c.ReadFromUDP(buf)
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		g.mu.Lock()
		g.peers[port] = addr
		g.mu.Unlock()
		g.frag.send(g.qc, port, buf[:n])
	}
}

func (g *tunnelGuest) datagrams() {
	var r reassembler
	for {
		d, err := g.qc.ReceiveDatagram(g.qc.Context())
		if err != nil {
			return
		}
		port, data, ok := r.add(d)
		if !ok {
			continue
		}
		c := g.udp[port]
		g.mu.Lock()
		addr := g.peers[port]
		g.mu.Unlock()
		if c != nil && addr != nil {
			c.WriteToUDP(data, addr)
		}
	}
}

// Done is closed when the tunnel ends.
func (g *tunnelGuest) Done() <-chan struct{} { return g.qc.Context().Done() }

// Close ends the tunnel and frees the ports.
func (g *tunnelGuest) Close() {
	g.closeOnce.Do(func() {
		for _, l := range g.tcp {
			l.Close()
		}
		for _, c := range g.udp {
			c.Close()
		}
		if g.qc != nil {
			g.qc.CloseWithError(0, "")
		}
		if g.tr != nil {
			g.tr.Close()
		}
		if g.agent != nil {
			g.agent.Close()
		}
	})
}

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// CheckServer validates the address of a signaling server as the user
// typed it in the settings: the Worker's URL from signal/. Empty means none.
func CheckServer(server string) error {
	if strings.TrimSpace(server) == "" {
		return nil
	}
	_, err := parseServer(server)
	return err
}

// WithKey joins the server's address and its access key, kept apart in the
// settings, into the address the host dials: the key goes as ?key=…. The
// key lets the host open rooms and never leaves this machine.
func WithKey(server, key string) string {
	server, key = strings.TrimSpace(server), strings.TrimSpace(key)
	if server == "" || key == "" {
		return server
	}
	u, err := url.Parse(server)
	if err != nil {
		return server
	}
	q := u.Query()
	q.Set("key", key)
	u.RawQuery = q.Encode()
	return u.String()
}

func parseServer(server string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(server))
	if err != nil || u.Host == "" {
		return nil, errors.New("la dirección del servidor de salas no es válida: debe ser como https://emuhub-signal.tu-cuenta.workers.dev")
	}
	switch u.Scheme {
	case "https", "wss":
		u.Scheme = "wss"
	case "http", "ws":
		u.Scheme = "ws"
	default:
		return nil, errors.New("la dirección del servidor de salas debe empezar por https://")
	}
	return u, nil
}

// roomURL is the WebSocket address of one room on the server. Only the host
// sends the server's key: guests are let in by knowing the room's name.
func roomURL(server string, secret []byte, role string) (string, error) {
	u, err := parseServer(server)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	if k := u.Query().Get("key"); k != "" && role == "host" {
		q.Set("key", k)
	}
	q.Set("role", role)
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/rooms/" + roomKey(secret)
	u.RawQuery = q.Encode()
	u.Fragment = ""
	return u.String(), nil
}

var (
	errBadServerKey = errors.New("el servidor de salas rechazó la clave: revisa la clave en Ajustes → Jugar con amigos")

	errNotAServer    = errors.New("la dirección de Ajustes → Jugar con amigos no es un servidor de salas de EmuHub")
	errServerTooLong = errors.New("la dirección del servidor de salas es demasiado larga")
)

// signalMsg is everything that travels through the signaling server. The
// server adds From to a guest's messages and routes the host's by To.
type signalMsg struct {
	Type   string   `json:"type"` // offer, answer or error
	From   string   `json:"from,omitempty"`
	To     string   `json:"to,omitempty"`
	ICE    *iceInfo `json:"ice,omitempty"`
	FP     string   `json:"fp,omitempty"`  // host certificate fingerprint
	MAC    string   `json:"mac,omitempty"` // proves the answer comes from the host
	Reason string   `json:"reason,omitempty"`
}

// signalConn is a WebSocket to one room on the signaling server.
type signalConn struct {
	ws *websocket.Conn
	mu sync.Mutex // one writer at a time
}

func dialSignal(ctx context.Context, server string, secret []byte, role string) (*signalConn, error) {
	addr, err := roomURL(server, secret, role)
	if err != nil {
		return nil, err
	}
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Proxy: http.ProxyFromEnvironment}
	ws, resp, err := d.DialContext(ctx, addr, http.Header{"User-Agent": {"emuhub"}})
	if err != nil {
		if resp != nil {
			resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusForbidden:
				return nil, errBadServerKey
			case http.StatusNotFound, http.StatusBadRequest, http.StatusUpgradeRequired:
				return nil, errNotAServer
			}
		}
		return nil, err
	}
	ws.SetReadLimit(64 << 10)
	return &signalConn{ws: ws}, nil
}

func (s *signalConn) send(m signalMsg) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return s.write(b)
}

// ping keeps the WebSocket from being dropped as idle. The server answers
// "pong" without waking up.
func (s *signalConn) ping() error { return s.write([]byte("ping")) }

func (s *signalConn) write(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return s.ws.WriteMessage(websocket.TextMessage, b)
}

// read returns the next message, skipping pongs and anything unreadable.
func (s *signalConn) read() (signalMsg, error) {
	for {
		_, b, err := s.ws.ReadMessage()
		if err != nil {
			return signalMsg{}, err
		}
		if string(b) == "pong" {
			continue
		}
		var m signalMsg
		if json.Unmarshal(b, &m) == nil && m.Type != "" {
			return m, nil
		}
	}
}

func (s *signalConn) Close() error { return s.ws.Close() }

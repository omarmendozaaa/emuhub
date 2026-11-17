package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// Tool is an external program (Sunshine or Moonlight) and how to start it.
type Tool struct {
	Exe  string
	Args []string // arguments that always come first
	Dir  string   // working directory; empty keeps EmuHub's
	// Isolated means the program ships with EmuHub, so its settings can live
	// apart from a copy the user installed themselves.
	Isolated bool
}

// Sunshine listens on several ports derived from one base port.
const defaultPort = 47989

// portOffsets are the TCP ports Sunshine opens relative to its base port:
// HTTPS, HTTP, web UI and RTSP.
var portOffsets = []int{-5, 0, 1, 21}

// freePort returns a base port whose TCP ports are all unused, starting at
// Sunshine's default so router rules written for it keep working. Ports are
// probed by connecting rather than listening, which on Windows would raise a
// firewall prompt for EmuHub itself.
func freePort() (uint16, error) {
	for base := defaultPort; base < defaultPort+1000; base += 100 {
		ok := true
		for _, off := range portOffsets {
			c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", base+off), 300*time.Millisecond)
			if err == nil {
				c.Close()
				ok = false
				break
			}
		}
		if ok {
			return uint16(base), nil
		}
	}
	return 0, fmt.Errorf("no hay puertos libres para la sala (probados desde el %d)", defaultPort)
}

// virtualPrefixes name interfaces that never lead to another player.
var virtualPrefixes = []string{"docker", "br-", "veth", "virbr", "lxc", "podman", "vmnet", "vboxnet"}

// localAddrs lists this machine's IPv4 addresses on real or VPN networks.
func localAddrs() []netip.Addr {
	var out []netip.Addr
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || isVirtual(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(ipn.IP.To4())
			if !ok || ip.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, ip)
		}
	}
	return out
}

func isVirtual(name string) bool {
	name = strings.ToLower(name)
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// inviteAddrs picks the addresses for the room code: this machine's local
// and VPN addresses, as many as fit.
func inviteAddrs(local []netip.Addr) []netip.Addr {
	return local[:min(len(local), maxAddrs)]
}

// reachable returns the first address, in invite order, where the host's
// Sunshine answers, together with the identifier that Sunshine reports.
func reachable(ctx context.Context, in Invite) (netip.Addr, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	type answer struct {
		ok bool
		id string
	}
	results := make([]chan answer, len(in.Addrs))
	client := &http.Client{}
	for i, addr := range in.Addrs {
		results[i] = make(chan answer, 1)
		go func(ch chan<- answer, url string) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				ch <- answer{}
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				ch <- answer{}
				return
			}
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			body := string(b)
			ch <- answer{ok: strings.Contains(body, "<hostname>"), id: xmlField(body, "uniqueid")}
		}(results[i], fmt.Sprintf("http://%s:%d/serverinfo", addr, in.Port))
	}
	for i, ch := range results {
		if a := <-ch; a.ok {
			return in.Addrs[i], a.id, nil
		}
	}
	return netip.Addr{}, "", errors.New("no se pudo conectar con el anfitrión. Comprueba que su sala sigue abierta y que estáis en la misma red o VPN")
}

// xmlField returns the text of the first <name> element in body, or "".
func xmlField(body, name string) string {
	_, rest, ok := strings.Cut(body, "<"+name+">")
	if !ok {
		return ""
	}
	value, _, ok := strings.Cut(rest, "</"+name+">")
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

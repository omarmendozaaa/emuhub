package remote

import (
	"context"
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

// publicIPServices answer with the caller's public IPv4 as plain text.
var publicIPServices = []string{"https://api.ipify.org", "https://ipv4.icanhazip.com"}

// publicAddr asks an external service for this network's public IPv4.
func publicAddr(ctx context.Context) (netip.Addr, bool) {
	client := &http.Client{Timeout: 4 * time.Second}
	for _, url := range publicIPServices {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "emuhub")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if ip, err := netip.ParseAddr(strings.TrimSpace(string(b))); err == nil && ip.Is4() && resp.StatusCode == http.StatusOK {
			return ip, true
		}
	}
	return netip.Addr{}, false
}

// inviteAddrs orders addresses for the room code: local ones first, so
// players on the same network or VPN never go out through the router.
func inviteAddrs(local []netip.Addr, public netip.Addr, hasPublic bool) []netip.Addr {
	var out []netip.Addr
	seen := map[netip.Addr]bool{}
	add := func(a netip.Addr) {
		if !seen[a] && len(out) < maxAddrs {
			seen[a] = true
			out = append(out, a)
		}
	}
	// Keep room for the public address.
	limit := maxAddrs
	if hasPublic {
		limit--
	}
	for _, a := range local {
		if len(out) < limit {
			add(a)
		}
	}
	if hasPublic {
		add(public)
	}
	return out
}

// reachable returns the first address, in invite order, where the host's
// Sunshine answers.
func reachable(ctx context.Context, in Invite) (netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	results := make([]chan bool, len(in.Addrs))
	client := &http.Client{}
	for i, addr := range in.Addrs {
		results[i] = make(chan bool, 1)
		go func(ch chan<- bool, url string) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				ch <- false
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				ch <- false
				return
			}
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			ch <- strings.Contains(string(b), "<hostname>")
		}(results[i], fmt.Sprintf("http://%s:%d/serverinfo", addr, in.Port))
	}
	for i, ch := range results {
		if <-ch {
			return in.Addrs[i], nil
		}
	}
	return netip.Addr{}, fmt.Errorf("no se pudo conectar con el anfitrión. Comprueba que su sala sigue abierta y, si jugáis por internet, que su router tiene UPnP o los puertos abiertos")
}

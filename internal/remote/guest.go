package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// GuestOptions configures joining a room.
type GuestOptions struct {
	Tool Tool   // Moonlight
	Dir  string // where a bundled Moonlight keeps its settings
	HD   bool   // 1080p instead of 720p; needs a faster connection on both sides
	// Name is how the host sees this guest in the room. Empty leaves it to
	// the host, who shows "Invitado N".
	Name string
	// Status reports progress in words for the UI.
	Status func(string)
}

// Guest is a Moonlight session connected to a host.
type Guest struct {
	cmd    *exec.Cmd
	output *tailBuffer
	tunnel *tunnelGuest // nil when Moonlight reaches the host directly
}

// Join reaches the host named by the room code, pairs with its PIN and starts
// streaming. It returns once Moonlight is running; Wait blocks until the
// session ends.
func Join(ctx context.Context, code string, opts GuestOptions) (*Guest, error) {
	in, err := DecodeInvite(code)
	if err != nil {
		return nil, err
	}
	status := opts.Status
	if status == nil {
		status = func(string) {}
	}

	status("Buscando la sala…")
	addr, hostID, err := reachable(ctx, in)
	var tunnel *tunnelGuest
	if err != nil {
		if len(in.Secret) == 0 {
			return nil, fmt.Errorf("%w. Para jugar desde otra red, el anfitrión necesita un servidor de salas en Ajustes → Jugar con amigos", err)
		}
		// No direct way in: meet the host on its signaling server.
		status("Abriendo un túnel hasta el anfitrión…")
		if tunnel, err = dialTunnel(ctx, in.Server, in, CleanName(opts.Name)); err != nil {
			return nil, err
		}
		addr, hostID, err = reachable(ctx, Invite{Addrs: []netip.Addr{tunnelListen}, Port: in.Port})
		if err != nil {
			tunnel.Close()
			return nil, err
		}
	}
	ok := false
	defer func() {
		if !ok && tunnel != nil {
			tunnel.Close()
		}
	}()
	host := fmt.Sprintf("%s:%d", addr, in.Port)
	if err := forgetOtherHosts(opts.settingsFile(), hostID); err != nil {
		return nil, fmt.Errorf("no se pudo preparar Moonlight: %w", err)
	}

	// A guest who comes back in the same session is still paired.
	if !paired(ctx, opts, host) {
		status("Emparejando con el anfitrión…")
		// Through the tunnel the name is added on the way already. Reaching
		// the host directly, pairing goes through a relay on loopback that
		// adds it; Moonlight then streams from the host's own address.
		pairWith := host
		if name := CleanName(opts.Name); name != "" && tunnel == nil {
			if relay, err := startPairRelay(addr, in.Port, name); err == nil {
				defer relay.Close()
				pairWith = relayHost(in.Port)
			}
		}
		pairCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		out, _ := opts.command(pairCtx, "pair", pairWith, "--pin", in.PIN).CombinedOutput()
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Moonlight exits with 0 either way, so ask the host what it thinks.
		if !paired(ctx, opts, host) {
			return nil, fmt.Errorf("ese código es de una sala que ya se cerró, o la sala está completa. Pide al anfitrión la invitación que aparece ahora en su sala\n%s", lastLines(out, 2))
		}
	}

	status("Conectando…")
	// The host is playing too, so its speakers must keep sounding.
	args := []string{"stream", host, AppName, "--no-quit-after", "--audio-on-host", "--display-mode", "fullscreen", "--fps", "60"}
	if opts.HD {
		args = append(args, "--1080", "--bitrate", "20000")
	} else {
		args = append(args, "--720", "--bitrate", "10000")
	}
	g := &Guest{output: &tailBuffer{max: 4096}, tunnel: tunnel}
	g.cmd = opts.command(context.Background(), args...)
	g.cmd.Stdout, g.cmd.Stderr = g.output, g.output
	if err := g.cmd.Start(); err != nil {
		return nil, fmt.Errorf("no se pudo iniciar Moonlight: %w", err)
	}
	ok = true
	return g, nil
}

// Tunneled reports whether the session goes through a tunnel.
func (g *Guest) Tunneled() bool { return g.tunnel != nil }

// Wait blocks until Moonlight closes and returns its last output lines when
// it ended with an error.
func (g *Guest) Wait() error {
	err := g.cmd.Wait()
	g.release()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, lastLines(g.output.Bytes(), 3))
	}
	return nil
}

// Stop closes the stream.
func (g *Guest) Stop() {
	if g.cmd.Process != nil {
		terminate(g.cmd)
	}
	g.release()
}

// release ends what the session holds besides Moonlight.
func (g *Guest) release() {
	if g.tunnel != nil {
		g.tunnel.Close()
	}
}

// paired reports whether the host lists its apps to this Moonlight, which it
// only does for paired clients.
func paired(ctx context.Context, opts GuestOptions, host string) bool {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var out bytes.Buffer
	cmd := opts.command(ctx, "list", host)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return false
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.TrimSpace(line) == AppName {
			return true
		}
	}
	return false
}

func (o GuestOptions) command(ctx context.Context, args ...string) *exec.Cmd {
	t := o.Tool
	cmd := exec.CommandContext(ctx, t.Exe, append(append([]string{}, t.Args...), args...)...)
	cmd.Dir = t.Dir
	cmd.Env = guestEnv(os.Environ(), t, o.Dir)
	return cmd
}

// guestEnv gives a bundled Moonlight its own settings folder on Linux, apart
// from a Moonlight the user may have installed. (On Windows the portable
// build already keeps its settings next to the executable.)
func guestEnv(env []string, t Tool, dir string) []string {
	if !t.Isolated || runtime.GOOS != "linux" || dir == "" {
		return env
	}
	home, _ := os.UserHomeDir()
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	// PulseAudio looks for its cookie under XDG_CONFIG_HOME too.
	if cookie := filepath.Join(config, "pulse", "cookie"); os.Getenv("PULSE_COOKIE") == "" {
		if _, err := os.Stat(cookie); err == nil {
			env = append(env, "PULSE_COOKIE="+cookie)
		}
	}
	return append(env, "XDG_CONFIG_HOME="+dir)
}

// settingsFile is where a bundled Moonlight saves the hosts it has met, or ""
// for a Moonlight the user installed, whose hosts are theirs to keep.
func (o GuestOptions) settingsFile() string {
	const folder = "Moonlight Game Streaming Project"
	switch {
	case !o.Tool.Isolated:
		return ""
	case runtime.GOOS == "linux" && o.Dir != "":
		return filepath.Join(o.Dir, folder, "Moonlight.conf")
	case runtime.GOOS == "windows" && o.Tool.Dir != "":
		// The portable build keeps its settings in its working directory.
		return filepath.Join(o.Tool.Dir, folder, "Moonlight.ini")
	}
	return ""
}

// forgetOtherHosts drops Moonlight's saved hosts when any of them is not the
// host being joined. Every room has a new identifier, and Moonlight cannot
// connect to an address it remembers under a different one: it waits half a
// minute and then reports a connection failure. The saved host is kept when
// it is this same room, so a guest who comes back is still paired.
func forgetOtherHosts(file, hostID string) error {
	if file == "" || hostID == "" {
		return nil
	}
	b, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var kept []string
	inHosts, stale := false, false
	for _, line := range strings.SplitAfter(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inHosts = strings.EqualFold(trimmed, "[hosts]")
		}
		if !inHosts {
			kept = append(kept, line)
			continue
		}
		if key, value, ok := strings.Cut(trimmed, "="); ok && strings.HasSuffix(key, `\uuid`) && !strings.EqualFold(value, hostID) {
			stale = true
		}
	}
	if !stale {
		return nil
	}
	return os.WriteFile(file, []byte(strings.Join(kept, "")), 0o600)
}

// ErrNoMoonlight and ErrNoSunshine are returned by callers that could not
// find the tools; kept here so messages stay in one place.
var (
	ErrNoMoonlight = errors.New("falta Moonlight para unirte a salas. Esta copia de EmuHub no lo incluye: instálalo desde moonlight-stream.org")
	ErrNoSunshine  = errors.New("falta Sunshine para crear salas. Esta copia de EmuHub no lo incluye: instálalo desde app.lizardbyte.dev/Sunshine")
)

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	buf bytes.Buffer
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if over := t.buf.Len() - t.max; over > 0 {
		t.buf.Next(over)
	}
	return len(p), nil
}

func (t *tailBuffer) Bytes() []byte { return t.buf.Bytes() }

func lastLines(b []byte, n int) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	// Status reports progress in words for the UI.
	Status func(string)
}

// Guest is a Moonlight session connected to a host.
type Guest struct {
	cmd    *exec.Cmd
	output *tailBuffer
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
	addr, err := reachable(ctx, in)
	if err != nil {
		return nil, err
	}
	host := fmt.Sprintf("%s:%d", addr, in.Port)

	// A guest who comes back in the same session is still paired.
	if !paired(ctx, opts, host) {
		status("Emparejando con el anfitrión…")
		pairCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		out, _ := opts.command(pairCtx, "pair", host, "--pin", in.PIN).CombinedOutput()
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Moonlight exits with 0 either way, so ask the host what it thinks.
		if !paired(ctx, opts, host) {
			return nil, fmt.Errorf("el anfitrión no aceptó la conexión. Pídele un código nuevo: caducan al cerrar la sala\n%s", lastLines(out, 2))
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
	g := &Guest{output: &tailBuffer{max: 4096}}
	g.cmd = opts.command(context.Background(), args...)
	g.cmd.Stdout, g.cmd.Stderr = g.output, g.output
	if err := g.cmd.Start(); err != nil {
		return nil, fmt.Errorf("no se pudo iniciar Moonlight: %w", err)
	}
	return g, nil
}

// Wait blocks until Moonlight closes and returns its last output lines when
// it ended with an error.
func (g *Guest) Wait() error {
	if err := g.cmd.Wait(); err != nil {
		return fmt.Errorf("%w\n%s", err, lastLines(g.output.Bytes(), 3))
	}
	return nil
}

// Stop closes the stream.
func (g *Guest) Stop() {
	if g.cmd.Process != nil {
		terminate(g.cmd)
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

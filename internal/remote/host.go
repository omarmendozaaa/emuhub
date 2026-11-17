package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// AppName is the single entry Sunshine offers to guests: the host's
	// screen, where EmuHub runs the emulator.
	AppName = "EmuHub"
	// MaxGuests fills every seat when the host only hosts.
	MaxGuests = 4
	// maxFailedPairings stops accepting guests after this many wrong PINs, so
	// the 4 digit PIN cannot be guessed by trying.
	maxFailedPairings = 5

	apiUser = "emuhub"
)

// HostOptions configures a hosting session.
type HostOptions struct {
	Tool Tool   // Sunshine
	Dir  string // where the session keeps Sunshine's config and logs
	// Internet opens the router ports with UPnP and adds the public address
	// to the room code. Without it the room only works on the local network
	// or a VPN.
	Internet bool
	// Guests is how many guests the room takes, up to MaxGuests.
	Guests int
	// OnGuest is called each time a guest pairs, with the number so far.
	OnGuest func(guests int)
	// OnLocked is called when the room stops accepting guests after too many
	// wrong PINs.
	OnLocked func()
}

// Host is a running Sunshine that pairs guests who present the session PIN.
type Host struct {
	opts    HostOptions
	cmd     *exec.Cmd
	exited  chan struct{}
	cancel  context.CancelFunc
	api     string
	pass    string
	client  *http.Client
	invite  Invite
	code    string
	warning string
	audio   *gameAudio
	stopped sync.Once

	mu     sync.Mutex
	guests int
	failed int
}

// HostSupported reports whether this system can host, and why not.
func HostSupported() error {
	switch runtime.GOOS {
	case "darwin":
		return errors.New("en macOS solo puedes unirte a salas: Sunshine no puede crear allí el mando del invitado")
	case "linux":
		f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
		if err != nil {
			return errors.New("EmuHub no puede crear el mando del invitado: tu usuario no tiene acceso a /dev/uinput. Ejecuta «sudo usermod -aG input $USER» y vuelve a iniciar sesión")
		}
		f.Close()
	}
	return nil
}

// StartHost launches Sunshine with a fresh identity and PIN and waits until
// it is ready to accept guests.
func StartHost(ctx context.Context, opts HostOptions) (*Host, error) {
	if err := HostSupported(); err != nil {
		return nil, err
	}
	if opts.Guests <= 0 || opts.Guests > MaxGuests {
		opts.Guests = MaxGuests
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	pin, err := randomPIN()
	if err != nil {
		return nil, err
	}
	pass, err := randomHex(16)
	if err != nil {
		return nil, err
	}

	h := &Host{
		opts:   opts,
		api:    fmt.Sprintf("https://127.0.0.1:%d", int(port)+1),
		pass:   pass,
		exited: make(chan struct{}),
		// Sunshine's web UI uses a self-signed certificate; only ever reached
		// here over loopback.
		client: &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}},
	}
	audio, audioErr := startGameAudio()
	if audioErr == nil {
		h.audio = audio
	}
	conf, err := h.writeConfig(port)
	if err != nil {
		h.Stop()
		return nil, err
	}

	// Sunshine stores a salted hash, so the API password is set through it.
	creds := h.command(conf, "--creds", apiUser, pass)
	if out, err := creds.CombinedOutput(); err != nil {
		h.Stop()
		return nil, fmt.Errorf("no se pudo preparar Sunshine: %w\n%s", err, tail(out, 600))
	}

	logFile, err := os.Create(filepath.Join(opts.Dir, "sunshine.out"))
	if err != nil {
		h.Stop()
		return nil, err
	}
	h.cmd = h.command(conf)
	h.cmd.Stdout, h.cmd.Stderr = logFile, logFile
	if err := h.cmd.Start(); err != nil {
		logFile.Close()
		h.cmd = nil
		h.Stop()
		return nil, fmt.Errorf("no se pudo iniciar Sunshine: %w", err)
	}
	go func() {
		h.cmd.Wait()
		logFile.Close()
		close(h.exited)
	}()

	if err := h.waitReady(ctx); err != nil {
		h.Stop()
		return nil, err
	}

	var public netip.Addr
	hasPublic := false
	if opts.Internet {
		public, hasPublic = publicAddr(ctx)
	}
	h.invite = Invite{Addrs: inviteAddrs(localAddrs(), public, hasPublic), Port: port, PIN: pin}
	if len(h.invite.Addrs) == 0 {
		h.Stop()
		return nil, errors.New("este equipo no está conectado a ninguna red")
	}
	if h.code, err = h.invite.Encode(); err != nil {
		h.Stop()
		return nil, err
	}
	if opts.Internet && !hasPublic {
		h.warning = "No se pudo averiguar tu dirección pública: la sala solo funcionará en tu red local o VPN."
	}
	if runtime.GOOS == "linux" && audioErr != nil {
		h.warning = "Los invitados oirán todo el audio de tu equipo, no solo el del juego (" + audioErr.Error() + ")."
	}
	if w := h.inputWarning(); w != "" {
		h.warning = w
	}

	loopCtx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go h.pairLoop(loopCtx)
	return h, nil
}

// Code is the room code to share with guests.
func (h *Host) Code() string { return h.code }

// Warning describes a problem that does not prevent hosting, if any.
func (h *Host) Warning() string { return h.warning }

// Guests is how many guests have paired in this session.
func (h *Host) Guests() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.guests
}

// GameEnv is what the game's environment needs while this room is open: on
// Linux it sends the game's sound to the sink guests hear, so they get only
// the game and not the rest of the host's desktop.
func (h *Host) GameEnv() []string {
	if h.audio == nil {
		return nil
	}
	return h.audio.env()
}

// GameStarted tells the room which process is the game, so its sound is the
// one guests hear.
func (h *Host) GameStarted(pid int) {
	if h.audio != nil {
		h.audio.claim(pid)
	}
}

// Exited is closed when Sunshine stops, whether asked to or not.
func (h *Host) Exited() <-chan struct{} { return h.exited }

// Stop ends the session. Guests are disconnected and their pairing is
// forgotten: the next session starts from a clean state.
func (h *Host) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
	if h.cmd != nil && h.cmd.Process != nil {
		terminate(h.cmd)
		select {
		case <-h.exited:
		case <-time.After(4 * time.Second):
			h.cmd.Process.Kill()
			<-h.exited
		}
	}
	h.stopped.Do(func() {
		if h.audio != nil {
			h.audio.stop()
		}
	})
}

func (h *Host) command(args ...string) *exec.Cmd {
	t := h.opts.Tool
	cmd := exec.Command(t.Exe, append(append([]string{}, t.Args...), args...)...)
	cmd.Dir = t.Dir
	// On Linux, Sunshine keeps its files in $CONFIGURATION_DIRECTORY/sunshine
	// instead of the user's own ~/.config/sunshine.
	cmd.Env = append(os.Environ(), "CONFIGURATION_DIRECTORY="+h.opts.Dir)
	configure(cmd)
	return cmd
}

// writeConfig creates Sunshine's files for this session and returns the path
// of its config. Pairings from earlier sessions are discarded.
func (h *Host) writeConfig(port uint16) (string, error) {
	dir := filepath.Join(h.opts.Dir, "sunshine")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	state := filepath.Join(dir, "sunshine_state.json")
	if err := os.Remove(state); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	apps := filepath.Join(dir, "apps.json")
	appsJSON, _ := json.MarshalIndent(map[string]any{
		"env":  map[string]string{},
		"apps": []map[string]string{{"name": AppName, "image-path": "desktop.png"}},
	}, "", "  ")
	if err := os.WriteFile(apps, appsJSON, 0o600); err != nil {
		return "", err
	}

	upnp := "disabled"
	if h.opts.Internet {
		upnp = "enabled"
	}
	lines := []string{
		"sunshine_name = " + AppName,
		fmt.Sprintf("port = %d", port),
		"upnp = " + upnp,
		// The web UI and its API answer to this machine only.
		"origin_web_ui_allowed = pc",
		// Guests bring a gamepad; they must not type or click on the host.
		"keyboard = disabled",
		"mouse = disabled",
		"controller = enabled",
		"system_tray = disabled",
		"file_apps = " + filepath.ToSlash(apps),
		"file_state = " + filepath.ToSlash(state),
		"credentials_file = " + filepath.ToSlash(state),
		"log_path = " + filepath.ToSlash(filepath.Join(dir, "sunshine.log")),
	}
	if h.audio != nil {
		// Only the game's sink, not everything the host's speakers play.
		lines = append(lines, "audio_sink = "+h.audio.sink())
	}
	if runtime.GOOS == "linux" {
		// An Xbox pad is the one every emulator's SDL mapping understands.
		lines = append(lines, "gamepad = xone")
		if c := captureMethod(os.Getenv("WAYLAND_DISPLAY"), os.Getenv("XDG_CURRENT_DESKTOP")); c != "" {
			lines = append(lines, "capture = "+c)
		}
	}
	conf := filepath.Join(dir, "sunshine.conf")
	return conf, os.WriteFile(conf, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

// wlrootsDesktops support wlr-screencopy, which captures without the
// screen-sharing prompt Sunshine would otherwise open on every start.
var wlrootsDesktops = []string{"hyprland", "sway", "river", "wayfire", "labwc", "niri"}

func captureMethod(waylandDisplay, desktop string) string {
	if waylandDisplay == "" {
		return ""
	}
	desktop = strings.ToLower(desktop)
	for _, d := range wlrootsDesktops {
		if strings.Contains(desktop, d) {
			return "wlr"
		}
	}
	return ""
}

// waitReady blocks until Sunshine's API answers. On desktops that ask before
// sharing the screen this includes the time the user takes to accept.
func (h *Host) waitReady(ctx context.Context) error {
	deadline := time.After(90 * time.Second)
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-h.exited:
			return fmt.Errorf("Sunshine se cerró al arrancar:\n%s", h.logTail())
		case <-deadline:
			return fmt.Errorf("Sunshine no respondió a tiempo. Si tu escritorio pidió permiso para compartir la pantalla, acéptalo y vuelve a intentarlo")
		case <-tick.C:
			var cfg struct {
				Status bool `json:"status"`
			}
			if err := h.call(ctx, http.MethodGet, "/api/config", nil, &cfg); err == nil {
				return nil
			}
		}
	}
}

func (h *Host) logTail() string {
	b, _ := os.ReadFile(filepath.Join(h.opts.Dir, "sunshine.out"))
	return tail(b, 600)
}

// call talks to Sunshine's local API.
func (h *Host) call(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.api+path, rd)
	if err != nil {
		return err
	}
	req.SetBasicAuth(apiUser, h.pass)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	// A rejected request (such as a wrong PIN) still carries a JSON answer.
	rejected := resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusConflict
	if resp.StatusCode != http.StatusOK && !(rejected && json.Valid(b)) {
		return fmt.Errorf("Sunshine respondió HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

// pairLoop accepts every pairing request with the session PIN. Sunshine and
// Moonlight then check that the guest typed the same PIN; a guest who did not
// fails there and counts toward the lock.
func (h *Host) pairLoop(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.exited:
			return
		case <-tick.C:
		}
		var pending struct {
			Pairings []struct {
				ID string `json:"id"`
			} `json:"pairings"`
		}
		if err := h.call(ctx, http.MethodGet, "/api/pin", nil, &pending); err != nil {
			continue
		}
		for _, p := range pending.Pairings {
			h.mu.Lock()
			n := h.guests
			h.mu.Unlock()
			if n >= h.opts.Guests {
				return
			}
			var res struct {
				Status bool `json:"status"`
			}
			req := map[string]string{"pairing_id": p.ID, "pin": h.invite.PIN, "name": fmt.Sprintf("Jugador %d", n+2)}
			if err := h.call(ctx, http.MethodPost, "/api/pin", req, &res); err != nil {
				continue
			}
			h.mu.Lock()
			if res.Status {
				h.guests++
			} else {
				h.failed++
			}
			guests, locked := h.guests, h.failed >= maxFailedPairings
			h.mu.Unlock()
			if res.Status && h.opts.OnGuest != nil {
				h.opts.OnGuest(guests)
			}
			if locked {
				if h.opts.OnLocked != nil {
					h.opts.OnLocked()
				}
				return
			}
		}
	}
}

// inputWarning reports a missing gamepad driver on Windows, where Sunshine
// needs one to create the guest's controller.
func (h *Host) inputWarning() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	type driver struct {
		Installed bool  `json:"installed"`
		Required  *bool `json:"requires_installed_driver"`
	}
	var st struct {
		ViGEmBus   driver `json:"vigembus"`
		VirtualHID driver `json:"virtualhid"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.call(ctx, http.MethodGet, "/api/virtual-input/status", nil, &st); err != nil {
		return ""
	}
	hidUsable := st.VirtualHID.Installed || (st.VirtualHID.Required != nil && !*st.VirtualHID.Required)
	if st.ViGEmBus.Installed || hidUsable {
		return ""
	}
	return "Falta el driver ViGEmBus: sin él, el mando del invitado no funcionará. Instálalo una vez desde github.com/nefarius/ViGEmBus/releases y vuelve a abrir la sala."
}

func randomPIN() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04d", n.Int64()), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func tail(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		s = "…" + s[len(s)-n:]
	}
	return s
}

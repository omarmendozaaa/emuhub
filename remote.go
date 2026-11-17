package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"time"

	"emuhub/internal/bundle"
	"emuhub/internal/library"
	"emuhub/internal/remote"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// RemoteState describes the current room, if any, for the UI.
type RemoteState struct {
	Role string `json:"role"` // "host", "guest" or "" when not playing with anyone
	// Phase is "starting" or "open" for a host, "connecting" or "playing"
	// for a guest.
	Phase   string `json:"phase"`
	GameID  string `json:"gameId"`
	Code    string `json:"code"`
	Guests  int    `json:"guests"`
	Message string `json:"message"`
	Warning string `json:"warning"`
	// Players is how many seats the host's room has and HostPlays whether
	// the host takes one of them.
	Players   int           `json:"players"`
	HostPlays bool          `json:"hostPlays"`
	Seats     []remote.Seat `json:"seats"`
	// SeatControl says EmuHub assigns each seat's gamepad in the emulator.
	// Without it the emulator numbers players as their gamepads show up.
	SeatControl bool `json:"seatControl"`
	// Multitap says this room needs the emulator's multitap.
	Multitap bool `json:"multitap"`
}

// RoomLimits tells the UI what a room for a game can be like.
type RoomLimits struct {
	MaxPlayers  int  `json:"maxPlayers"`
	SeatControl bool `json:"seatControl"`
	// MultitapOver is the player count above which the emulator's multitap
	// is used, or 0 when it never is.
	MultitapOver int `json:"multitapOver"`
}

// RemoteSupport tells the UI what this machine can do, with the reason when
// it cannot.
type RemoteSupport struct {
	Host        bool   `json:"host"`
	HostReason  string `json:"hostReason"`
	Guest       bool   `json:"guest"`
	GuestReason string `json:"guestReason"`
}

// remoteSession is the state behind RemoteState; guarded by App.mu.
type remoteSession struct {
	state  RemoteState
	host   *remote.Host
	guest  *remote.Guest
	cancel context.CancelFunc
	// hostPads is how many gamepads the host had when the room opened; the
	// guests' gamepads are numbered after them.
	hostPads int
}

// remoteTool finds Sunshine or Moonlight: the copy shipped with EmuHub, or
// one installed on the system.
func (a *App) remoteTool(id string, system ...string) (remote.Tool, bool) {
	a.mu.Lock()
	m := a.bundle
	a.mu.Unlock()
	if e := m.ByID(id); e != nil {
		if args := library.SplitCommand(e.Command); len(args) > 0 {
			exe := e.Expand(args[0], nil)
			dir := filepath.Dir(exe)
			if goruntime.GOOS == "linux" {
				// Unpacked AppImages look for their files from their root.
				dir = e.Root
			}
			return remote.Tool{Exe: exe, Dir: dir, Isolated: true}, true
		}
	}
	for _, name := range system {
		if p, err := exec.LookPath(name); err == nil {
			return remote.Tool{Exe: p}, true
		}
	}
	return remote.Tool{}, false
}

func (a *App) sunshine() (remote.Tool, bool) {
	return a.remoteTool("sunshine", "sunshine", `C:\Program Files\Sunshine\sunshine.exe`)
}

func (a *App) moonlight() (remote.Tool, bool) {
	return a.remoteTool("moonlight", "moonlight", "moonlight-qt",
		`C:\Program Files\Moonlight Game Streaming\Moonlight.exe`,
		"/Applications/Moonlight.app/Contents/MacOS/Moonlight")
}

func (a *App) GetRemoteSupport() RemoteSupport {
	var s RemoteSupport
	if err := remote.HostSupported(); err != nil {
		s.HostReason = err.Error()
	} else if _, ok := a.sunshine(); !ok {
		s.HostReason = a.missingTool(remote.ErrNoSunshine)
	} else {
		s.Host = true
	}
	if _, ok := a.moonlight(); !ok {
		s.GuestReason = a.missingTool(remote.ErrNoMoonlight)
	} else {
		s.Guest = true
	}
	return s
}

// missingTool explains a missing tool, which in a copy that ships them only
// means the bundle is still being loaded or unpacked, or failed to unpack.
func (a *App) missingTool(err error) string {
	if !bundle.Available() {
		return err.Error()
	}
	info := a.GetBundleInfo()
	if info.Error != "" {
		return info.Error
	}
	if !info.Ready {
		return "los programas incluidos aún se están preparando, espera un momento"
	}
	return err.Error()
}

func (a *App) GetRemoteState() RemoteState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.remote.state
}

// setRemote replaces the room state and tells the UI.
func (a *App) setRemote(fn func(*remoteSession)) {
	a.mu.Lock()
	fn(&a.remote)
	state := a.remote.state
	a.mu.Unlock()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "remote:state", state)
	}
}

func (a *App) remoteNotice(text, kind string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "remote:notice", map[string]string{"text": text, "kind": kind})
	}
}

func remoteDir(name string) (string, error) {
	base, err := bundle.DataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "remote", name)
	return dir, os.MkdirAll(dir, 0o700)
}

// ---- Hosting ----

// seatEmulator returns the game's emulator when EmuHub can assign its
// gamepads to seats.
func (a *App) seatEmulator(g library.Game) *bundle.Emulator {
	if a.store.ConsoleConfig(g.Console).Emulator != "bundled" {
		return nil
	}
	if emu := a.bundledFor(g.Console); emu.MaxSeats() > 0 {
		return emu
	}
	return nil
}

// padsAtStart reports whether the game's emulator only sees the gamepads
// connected when the game starts.
func (a *App) padsAtStart(g library.Game) bool {
	if a.store.ConsoleConfig(g.Console).Emulator != "bundled" {
		return false
	}
	emu := a.bundledFor(g.Console)
	return emu != nil && emu.PadsAtStart
}

// guestPadWait is how long a launch waits for the guests' gamepads.
const guestPadWait = 30 * time.Second

// waitGuestPads holds back an emulator that only sees the gamepads present
// at start until every guest's pad exists. Sunshine creates a guest's pad
// when they first press a button, not when they join, so a game started
// right away would leave them out until it restarts.
func (a *App) waitGuestPads(g library.Game) {
	if !a.padsAtStart(g) {
		return
	}
	missing := func() (int, bool) {
		a.mu.Lock()
		defer a.mu.Unlock()
		s := a.remote.state
		if s.Role != "host" || s.GameID != g.ID || a.remote.host == nil {
			return 0, false
		}
		return a.remote.hostPads + s.Guests - remote.Gamepads(), true
	}
	n, ok := missing()
	if !ok || n <= 0 {
		return
	}
	a.remoteNotice("Esperando los mandos de los invitados: pide a cada uno que pulse un botón de su mando", "info")
	deadline := time.Now().Add(guestPadWait)
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		if n, ok = missing(); !ok || n <= 0 {
			return
		}
	}
	a.remoteNotice(fmt.Sprintf("Faltan %d mandos de invitados: el emulador no los verá hasta que reinicies el juego", n), "error")
}

// roomGameEnv is what the game's environment needs while this machine hosts
// a room, such as sending its sound to the sink guests hear.
func (a *App) roomGameEnv() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.remote.state.Role != "host" || a.remote.host == nil {
		return nil
	}
	return a.remote.host.GameEnv()
}

// roomGameStarted tells the open room, if any, which process is the game.
func (a *App) roomGameStarted(pid int) {
	a.mu.Lock()
	host := a.remote.host
	if a.remote.state.Role != "host" {
		host = nil
	}
	a.mu.Unlock()
	if host != nil {
		host.GameStarted(pid)
	}
}

func (a *App) roomLimits(g library.Game) RoomLimits {
	l := RoomLimits{MaxPlayers: 2}
	if c := library.ConsoleByID(g.Console); c != nil && c.MaxPlayers > 0 {
		l.MaxPlayers = c.MaxPlayers
	}
	if emu := a.seatEmulator(g); emu != nil {
		l.SeatControl = true
		l.MaxPlayers = emu.MaxSeats()
		if s := emu.FirstRun.Seats; s.Multitap != nil && len(s.Multitap.Sections) > len(s.Sections) {
			l.MultitapOver = len(s.Sections)
		}
	}
	l.MaxPlayers = min(l.MaxPlayers, remote.MaxSeats)
	return l
}

// GetRoomLimits tells how many players a room for this game can have.
func (a *App) GetRoomLimits(id string) (RoomLimits, error) {
	g, ok := a.store.Game(id)
	if !ok {
		return RoomLimits{}, errors.New("juego no encontrado")
	}
	return a.roomLimits(g), nil
}

// HostGame opens a room for a game: it starts streaming this screen and
// returns the code to share. players is how many seats the room has and
// hostPlays whether the host takes one. The game itself starts with
// LaunchGame, so the host can wait for the guests first.
func (a *App) HostGame(id string, players int, hostPlays bool) (RemoteState, error) {
	g, ok := a.store.Game(id)
	if !ok {
		return RemoteState{}, errors.New("juego no encontrado")
	}
	limits := a.roomLimits(g)
	players = remote.ClampPlayers(players, hostPlays, limits.MaxPlayers)
	if a.RunningGame() != "" {
		return RemoteState{}, errors.New("cierra el juego en ejecución antes de abrir una sala")
	}
	// Fail now, not with a guest waiting, if the game cannot start.
	if _, err := a.prepareLaunch(g); err != nil {
		return RemoteState{}, err
	}
	tool, ok := a.sunshine()
	if !ok {
		return RemoteState{}, errors.New(a.missingTool(remote.ErrNoSunshine))
	}
	dir, err := remoteDir("host")
	if err != nil {
		return RemoteState{}, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	if a.remote.state.Role != "" {
		a.mu.Unlock()
		cancel()
		return RemoteState{}, errors.New("ya hay una sala abierta")
	}
	a.remote = remoteSession{
		state: RemoteState{
			Role: "host", Phase: "starting", GameID: id,
			Players: players, HostPlays: hostPlays, Seats: remote.NewSeats(players, hostPlays),
			SeatControl: limits.SeatControl,
			Multitap:    limits.MultitapOver > 0 && players > limits.MultitapOver,
		},
		cancel:   cancel,
		hostPads: remote.Gamepads(),
	}
	state := a.remote.state
	a.mu.Unlock()
	runtime.EventsEmit(a.ctx, "remote:state", state)

	host, err := remote.StartHost(ctx, remote.HostOptions{
		Tool:     tool,
		Dir:      dir,
		Internet: !a.store.Settings().RemoteLANOnly,
		Guests:   remote.GuestSeats(players, hostPlays),
		OnGuest: func(n int) {
			seat := 0
			a.setRemote(func(s *remoteSession) {
				s.state.Guests = n
				// The UI gets its own copy: it reads the old one meanwhile.
				seats := append([]remote.Seat{}, s.state.Seats...)
				if remote.SeatGuest(seats, n) {
					s.state.Seats = seats
				}
				for i, st := range seats {
					if st.Kind == "guest" && st.Guest == n {
						seat = i + 1
					}
				}
			})
			if a.RunningGame() == id && a.padsAtStart(g) {
				a.remoteNotice(fmt.Sprintf("Invitado %d se ha unido con el juego en marcha: el emulador no verá su mando hasta que reinicies el juego", n), "error")
			} else if seat > 0 {
				a.remoteNotice(fmt.Sprintf("Invitado %d se ha unido: jugador %d", n, seat), "info")
			} else {
				a.remoteNotice("Un invitado se ha unido a la sala", "info")
			}
		},
		OnLocked: func() {
			a.setRemote(func(s *remoteSession) {
				s.state.Warning = "La sala dejó de aceptar invitados tras varios códigos incorrectos. Ciérrala y ábrela de nuevo para invitar a alguien más."
			})
		},
	})
	if err != nil {
		cancelled := ctx.Err() != nil
		cancel()
		a.setRemote(func(s *remoteSession) { *s = remoteSession{} })
		if cancelled {
			return RemoteState{}, errors.New("sala cancelada")
		}
		return RemoteState{}, err
	}

	a.mu.Lock()
	if ctx.Err() != nil {
		// Closed while Sunshine was starting.
		a.mu.Unlock()
		host.Stop()
		return RemoteState{}, errors.New("sala cancelada")
	}
	a.remote.host = host
	a.remote.state.Phase = "open"
	a.remote.state.Code = host.Code()
	a.remote.state.Warning = host.Warning()
	state = a.remote.state
	a.mu.Unlock()
	runtime.EventsEmit(a.ctx, "remote:state", state)

	go func() {
		<-host.Exited()
		a.mu.Lock()
		mine := a.remote.host == host
		if mine {
			a.remote = remoteSession{}
		}
		a.mu.Unlock()
		if mine {
			runtime.EventsEmit(a.ctx, "remote:state", RemoteState{})
			a.remoteNotice("La sala se cerró inesperadamente: Sunshine dejó de funcionar", "error")
		}
	}()
	return state, nil
}

// MoveSeat swaps a seat with the one before (delta -1) or after (delta 1) it
// and returns where it ended up. Seats are fixed once the game starts: the
// emulator has read its gamepad assignment by then.
func (a *App) MoveSeat(index, delta int) int {
	if a.RunningGame() != "" {
		return index
	}
	pos := index
	a.setRemote(func(s *remoteSession) {
		if s.state.Role != "host" || !s.state.SeatControl {
			return
		}
		seats := append([]remote.Seat{}, s.state.Seats...)
		pos = remote.MoveSeat(seats, index, delta)
		s.state.Seats = seats
	})
	return pos
}

// roomSeats returns the gamepad of each seat when a room is open for this
// game, in the emulator's numbering.
func (a *App) roomSeats(gameID string) ([]int, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.remote.state
	if s.Role != "host" || s.GameID != gameID || !s.SeatControl || len(s.Seats) == 0 {
		return nil, false
	}
	return remote.PadIndexes(s.Seats, a.remote.hostPads), true
}

// StopHosting closes the room. A running game keeps going for the host.
func (a *App) StopHosting() {
	a.mu.Lock()
	if a.remote.state.Role != "host" {
		a.mu.Unlock()
		return
	}
	s := a.remote
	a.remote = remoteSession{}
	a.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.host != nil {
		s.host.Stop()
	}
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "remote:state", RemoteState{})
	}
}

// ---- Joining ----

// JoinGame connects to a friend's room with the code they shared. It returns
// once the code is accepted as well formed; progress and the end of the
// session arrive as events.
func (a *App) JoinGame(code string) error {
	if _, err := remote.DecodeInvite(code); err != nil {
		return err
	}
	if a.RunningGame() != "" {
		return errors.New("cierra el juego en ejecución antes de unirte a una sala")
	}
	tool, ok := a.moonlight()
	if !ok {
		return errors.New(a.missingTool(remote.ErrNoMoonlight))
	}
	dir, err := remoteDir("moonlight")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	if a.remote.state.Role != "" {
		a.mu.Unlock()
		cancel()
		return errors.New("ya estás en una sala")
	}
	a.remote = remoteSession{state: RemoteState{Role: "guest", Phase: "connecting", Message: "Buscando la sala…"}, cancel: cancel}
	state := a.remote.state
	a.mu.Unlock()
	runtime.EventsEmit(a.ctx, "remote:state", state)

	go func() {
		defer cancel()
		end := func(msg string) {
			a.mu.Lock()
			mine := a.remote.state.Role == "guest"
			if mine {
				a.remote = remoteSession{}
			}
			a.mu.Unlock()
			if !mine {
				return
			}
			runtime.EventsEmit(a.ctx, "remote:state", RemoteState{})
			if msg != "" {
				a.remoteNotice(msg, "error")
			}
		}
		guest, err := remote.Join(ctx, code, remote.GuestOptions{
			Tool: tool,
			Dir:  dir,
			HD:   a.store.Settings().RemoteHD,
			Status: func(msg string) {
				a.setRemote(func(s *remoteSession) { s.state.Message = msg })
			},
		})
		if err != nil {
			if ctx.Err() != nil {
				end("")
			} else {
				end(err.Error())
			}
			return
		}
		a.mu.Lock()
		if ctx.Err() != nil {
			a.mu.Unlock()
			guest.Stop()
			guest.Wait()
			end("")
			return
		}
		a.remote.guest = guest
		a.remote.state.Phase = "playing"
		a.remote.state.Message = ""
		state := a.remote.state
		a.mu.Unlock()
		runtime.EventsEmit(a.ctx, "remote:state", state)
		if a.store.Settings().MinimizeOnLaunch {
			runtime.WindowMinimise(a.ctx)
		}

		// Moonlight reports connection problems in its own window.
		guest.Wait()
		runtime.WindowUnminimise(a.ctx)
		runtime.WindowShow(a.ctx)
		end("")
	}()
	return nil
}

// LeaveGame disconnects from the room this player joined.
func (a *App) LeaveGame() {
	a.mu.Lock()
	if a.remote.state.Role != "guest" {
		a.mu.Unlock()
		return
	}
	cancel, guest := a.remote.cancel, a.remote.guest
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if guest != nil {
		guest.Stop()
	}
}

// shutdown closes any room so nothing keeps streaming after EmuHub quits.
func (a *App) shutdown(context.Context) {
	a.StopHosting()
	a.LeaveGame()
}

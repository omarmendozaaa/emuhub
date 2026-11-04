package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"emuhub/internal/bundle"
	"emuhub/internal/library"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx   context.Context
	store *library.Store

	mu          sync.Mutex
	running     *exec.Cmd
	runningID   string
	coversBusy  bool
	bundle      *bundle.Manifest // installed bundled emulators; nil until ready
	bundleState BundleState
}

type ConsoleInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	ShortName  string   `json:"shortName"`
	Maker      string   `json:"maker"`
	Accent     string   `json:"accent"`
	Extensions []string `json:"extensions"`
	Emulator   string   `json:"emulator"` // "bundled" or "" for a custom command
	Command    string   `json:"command"`
	RomDirs    []string `json:"romDirs"`
	// Bundled names the emulator shipped for this console, if any.
	Bundled      *BundledEmuInfo     `json:"bundled"`
	EmulatorOK   bool                `json:"emulatorOk"`
	Requirements []RequirementStatus `json:"requirements"`
	Ready        bool                `json:"ready"`
}

type CoverProgress struct {
	Done  int  `json:"done"`
	Total int  `json:"total"`
	Found int  `json:"found"`
	Busy  bool `json:"busy"`
}

func NewApp(store *library.Store) *App {
	return &App{store: store}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := library.EnsureBIOSDirs(); err != nil {
		log.Printf("no se pudieron crear las carpetas de BIOS: %v", err)
	}
	a.autoConfigure()
	go a.installBundle()
	if a.store.Settings().AutoCovers {
		go a.fetchMissingCovers()
	}
}

// autoConfigure fills in emulators and conventional ROM folders for consoles
// the user has not configured yet.
func (a *App) autoConfigure() {
	home, _ := os.UserHomeDir()
	embedded, _ := bundle.Embedded()
	for _, c := range library.Consoles {
		cfg := a.store.ConsoleConfig(c.ID)
		changed := false
		if cfg.Emulator == "" && cfg.Command == "" && embedded.ForConsole(c.ID) != nil {
			cfg.Emulator = "bundled"
			changed = true
		}
		if cfg.Emulator == "" && cfg.Command == "" {
			if found := library.DetectEmulators(c); len(found) > 0 {
				cfg.Command = found[0].Command
				changed = true
			}
		}
		if len(cfg.RomDirs) == 0 {
			guesses := []string{}
			for _, base := range []string{"ROMs", "Roms", "roms", "Games", "Emulation/roms"} {
				for _, name := range []string{c.ID, strings.ToUpper(c.ID), c.ShortName} {
					guesses = append(guesses, filepath.Join(home, base, name))
				}
			}
			if c.ID == "ps3" {
				guesses = append(guesses,
					filepath.Join(home, ".config/rpcs3/dev_hdd0/game"),
					filepath.Join(home, ".config/rpcs3/games"),
					filepath.Join(home, ".var/app/net.rpcs3.RPCS3/config/rpcs3/dev_hdd0/game"))
			}
			for _, g := range guesses {
				if st, err := os.Stat(g); err == nil && st.IsDir() && !contains(cfg.RomDirs, g) {
					cfg.RomDirs = append(cfg.RomDirs, g)
					changed = true
				}
			}
		}
		if changed {
			a.store.SetConsoleConfig(c.ID, cfg)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ---- Library ----

func (a *App) GetConsoles() []ConsoleInfo {
	embedded, _ := bundle.Embedded()
	out := make([]ConsoleInfo, 0, len(library.Consoles))
	for _, c := range library.Consoles {
		cfg := a.store.ConsoleConfig(c.ID)
		info := ConsoleInfo{
			ID: c.ID, Name: c.Name, ShortName: c.ShortName, Maker: c.Maker, Accent: c.Accent, Extensions: c.Extensions,
			Emulator: cfg.Emulator, Command: cfg.Command, RomDirs: cfg.RomDirs,
		}
		if e := embedded.ForConsole(c.ID); e != nil {
			info.Bundled = &BundledEmuInfo{Name: e.Name, Version: e.Version, License: e.License, Source: e.Source, Consoles: e.Consoles}
		}
		var emu *bundle.Emulator
		if cfg.Emulator == "bundled" {
			emu = a.bundledFor(c.ID)
			info.EmulatorOK = emu != nil
		} else {
			info.EmulatorOK = commandAvailable(cfg.Command)
		}
		info.Requirements = a.requirementStatuses(c.ID, emu)
		info.Ready = info.EmulatorOK
		for _, r := range info.Requirements {
			info.Ready = info.Ready && r.OK
		}
		out = append(out, info)
	}
	return out
}

func commandAvailable(command string) bool {
	args := library.SplitCommand(command)
	if len(args) == 0 {
		return false
	}
	_, err := exec.LookPath(args[0])
	return err == nil
}

func (a *App) GetGames() []library.Game {
	return a.store.Games()
}

func (a *App) GetSettings() library.Settings {
	return a.store.Settings()
}

func (a *App) SaveSettings(s library.Settings) error {
	return a.store.SetSettings(s)
}

func (a *App) SaveConsoleConfig(id, emulator, command string, romDirs []string) error {
	if library.ConsoleByID(id) == nil {
		return fmt.Errorf("consola desconocida: %s", id)
	}
	if emulator != "bundled" {
		emulator = ""
	}
	return a.store.SetConsoleConfig(id, library.ConsoleConfig{Emulator: emulator, Command: strings.TrimSpace(command), RomDirs: romDirs})
}

func (a *App) DetectEmulators(id string) []library.DetectedEmulator {
	c := library.ConsoleByID(id)
	if c == nil {
		return nil
	}
	return library.DetectEmulators(c)
}

// ScanLibrary rescans every console's ROM folders and returns the number of
// games found. Missing box art is downloaded in the background when enabled.
func (a *App) ScanLibrary() (int, error) {
	total := 0
	for _, c := range library.Consoles {
		games := library.Scan(c, a.store.ConsoleConfig(c.ID).RomDirs)
		if err := a.store.ReplaceConsoleGames(c.ID, games); err != nil {
			return total, err
		}
		total += len(games)
	}
	runtime.EventsEmit(a.ctx, "library:changed")
	if a.store.Settings().AutoCovers {
		go a.fetchMissingCovers()
	}
	return total, nil
}

func (a *App) FetchCovers() {
	go a.fetchMissingCovers()
}

func (a *App) fetchMissingCovers() {
	a.mu.Lock()
	if a.coversBusy {
		a.mu.Unlock()
		return
	}
	a.coversBusy = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.coversBusy = false
		a.mu.Unlock()
	}()

	var missing []library.Game
	for _, g := range a.store.Games() {
		if !g.HasCover {
			missing = append(missing, g)
		}
	}
	if len(missing) == 0 {
		return
	}
	p := CoverProgress{Total: len(missing), Busy: true}
	runtime.EventsEmit(a.ctx, "covers:progress", p)
	dir := filepath.Join(a.store.Dir(), "covers")
	lastEmit := time.Now()
	for _, g := range missing {
		if path, err := library.FetchCover(a.ctx, g, dir); err == nil {
			a.store.UpdateGame(g.ID, func(x *library.Game) { x.Cover = path })
			p.Found++
		}
		p.Done++
		runtime.EventsEmit(a.ctx, "covers:progress", p)
		if time.Since(lastEmit) > 1500*time.Millisecond {
			runtime.EventsEmit(a.ctx, "library:changed")
			lastEmit = time.Now()
		}
	}
	p.Busy = false
	runtime.EventsEmit(a.ctx, "covers:progress", p)
	runtime.EventsEmit(a.ctx, "library:changed")
}

func (a *App) ToggleFavorite(id string) (library.Game, error) {
	return a.store.UpdateGame(id, func(g *library.Game) { g.Favorite = !g.Favorite })
}

func (a *App) RemoveGame(id string) error {
	return a.store.RemoveGame(id)
}

// ChooseCover lets the user pick an image file as the game's cover.
func (a *App) ChooseCover(id string) (library.Game, error) {
	g, ok := a.store.Game(id)
	if !ok {
		return library.Game{}, errors.New("juego no encontrado")
	}
	src, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Elegir portada para " + g.Title,
		Filters: []runtime.FileFilter{{DisplayName: "Imágenes", Pattern: "*.png;*.jpg;*.jpeg;*.webp"}},
	})
	if err != nil || src == "" {
		return g, err
	}
	dst, err := library.CopyCover(src, filepath.Join(a.store.Dir(), "covers"), g)
	if err != nil {
		return g, err
	}
	return a.store.UpdateGame(id, func(x *library.Game) { x.Cover = dst })
}

// ---- Dialogs ----

func (a *App) PickFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Carpeta de juegos"})
}

func (a *App) PickExecutable() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Ejecutable del emulador"})
}

// ---- Launching ----

func (a *App) LaunchGame(id string) error {
	g, ok := a.store.Game(id)
	if !ok {
		return errors.New("juego no encontrado")
	}
	if a.RunningGame() != "" {
		return errors.New("ya hay un juego en ejecución")
	}
	args, err := a.prepareLaunch(g)
	if err != nil {
		return err
	}

	a.mu.Lock()
	if a.running != nil {
		a.mu.Unlock()
		return errors.New("ya hay un juego en ejecución")
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = filepath.Dir(g.Path)
	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = &tailWriter{buf: &stderr, max: 4096}
	if err := cmd.Start(); err != nil {
		a.mu.Unlock()
		return fmt.Errorf("no se pudo iniciar el emulador: %w", err)
	}
	a.running, a.runningID = cmd, id
	a.mu.Unlock()

	start := time.Now()
	a.store.UpdateGame(id, func(x *library.Game) { x.LastPlayed = start.Unix() })
	runtime.EventsEmit(a.ctx, "game:started", id)
	if a.store.Settings().MinimizeOnLaunch {
		runtime.WindowMinimise(a.ctx)
	}

	go func() {
		err := cmd.Wait()
		elapsed := time.Since(start)
		a.mu.Lock()
		a.running, a.runningID = nil, ""
		a.mu.Unlock()
		a.store.UpdateGame(id, func(x *library.Game) { x.PlaySeconds += int64(elapsed.Seconds()) })

		msg := ""
		// A quick non-zero exit almost always means a bad config or missing BIOS.
		if err != nil && elapsed < 5*time.Second {
			msg = strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
		}
		runtime.WindowUnminimise(a.ctx)
		runtime.WindowShow(a.ctx)
		runtime.EventsEmit(a.ctx, "game:exited", map[string]any{"id": id, "seconds": int64(elapsed.Seconds()), "error": msg})
		runtime.EventsEmit(a.ctx, "library:changed")
	}()
	return nil
}

func (a *App) StopGame() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running != nil && a.running.Process != nil {
		a.running.Process.Signal(os.Interrupt)
	}
}

func (a *App) RunningGame() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runningID
}

func (a *App) Quit() {
	runtime.Quit(a.ctx)
}

// tailWriter keeps only the last max bytes written, enough to show the
// emulator's final error message.
type tailWriter struct {
	buf *bytes.Buffer
	max int
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	if over := w.buf.Len() - w.max; over > 0 {
		w.buf.Next(over)
	}
	return len(p), nil
}

// ---- Media ----

// mediaMiddleware serves cover and backdrop images at /media/{kind}/{id}.
func (a *App) mediaMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/media/")
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		kind, id, _ := strings.Cut(rest, "/")
		g, found := a.store.Game(id)
		if !found {
			http.NotFound(w, r)
			return
		}
		path := g.Cover
		if kind == "backdrop" {
			path = g.Backdrop
		}
		if path == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "max-age=3600")
		http.ServeFile(w, r, path)
	})
}

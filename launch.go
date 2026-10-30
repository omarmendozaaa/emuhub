package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"emuhub/internal/bundle"
	"emuhub/internal/library"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// requirementErrorPrefix marks launch errors the frontend answers by opening
// that console's settings ("REQ:<console>:<message>").
const requirementErrorPrefix = "REQ:"

func requirementError(console, format string, args ...any) error {
	return fmt.Errorf("%s%s:%s", requirementErrorPrefix, console, fmt.Sprintf(format, args...))
}

// ---- Bundled emulators ----

type BundleState struct {
	Available  bool             `json:"available"`
	Installing bool             `json:"installing"`
	Ready      bool             `json:"ready"`
	Done       int64            `json:"done"`
	Total      int64            `json:"total"`
	Error      string           `json:"error"`
	Version    string           `json:"version"`
	Emulators  []BundledEmuInfo `json:"emulators"`
}

type BundledEmuInfo struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	License  string   `json:"license"`
	Source   string   `json:"source"`
	Consoles []string `json:"consoles"`
}

func (a *App) GetBundleInfo() BundleState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bundleState
}

// installBundle unpacks the embedded emulators on first run (or after an
// update that changes the bundle version) and reports progress to the UI.
func (a *App) installBundle() {
	m, err := bundle.Embedded()
	if err != nil {
		return
	}
	state := BundleState{Available: true, Version: m.BundleVersion}
	for _, e := range m.Emulators {
		state.Emulators = append(state.Emulators, BundledEmuInfo{Name: e.Name, Version: e.Version, License: e.License, Source: e.Source, Consoles: e.Consoles})
	}
	dir, err := bundle.DataDir()
	if err != nil {
		state.Error = err.Error()
		a.setBundleState(state)
		return
	}
	if installed, ok := bundle.Installed(dir); ok {
		state.Ready = true
		a.mu.Lock()
		a.bundle = installed
		a.bundleState = state
		a.mu.Unlock()
		return
	}

	state.Installing = true
	a.setBundleState(state)
	last := int64(-1)
	installed, err := bundle.Install(dir, func(done, total int64) {
		// Throttle events to whole percents.
		if pct := done * 100 / max(total, 1); pct != last {
			last = pct
			state.Done, state.Total = done, total
			a.setBundleState(state)
		}
	})
	state.Installing = false
	if err != nil {
		state.Error = "No se pudieron preparar los emuladores: " + err.Error()
	} else {
		state.Ready = true
		a.mu.Lock()
		a.bundle = installed
		a.mu.Unlock()
	}
	a.setBundleState(state)
}

func (a *App) setBundleState(s BundleState) {
	a.mu.Lock()
	a.bundleState = s
	a.mu.Unlock()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "bundle:state", s)
	}
}

// bundledFor returns the installed bundled emulator for a console.
func (a *App) bundledFor(console string) *bundle.Emulator {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bundle.ForConsole(console)
}

// ---- Requirements (BIOS / firmware) ----

type RequirementStatus struct {
	ID     string   `json:"id"`
	Label  string   `json:"label"`
	Kind   string   `json:"kind"`
	Hint   string   `json:"hint"`
	Path   string   `json:"path"`
	OK     bool     `json:"ok"`
	Detail string   `json:"detail"`
	Files  []string `json:"files"`
}

func (a *App) requirementStatuses(console string, emu *bundle.Emulator) []RequirementStatus {
	paths := a.store.Settings().Paths
	out := []RequirementStatus{}
	for _, r := range library.RequirementsFor(console) {
		st := RequirementStatus{ID: r.ID, Label: r.Label, Kind: r.Kind, Hint: r.Hint, Path: paths[r.ID]}
		files, err := r.Check(st.Path)
		switch {
		case err == nil:
			st.OK, st.Files = true, files
			st.Detail = fmt.Sprintf("%d archivo(s) válido(s)", len(files))
		case firmwareInstalled(emu, r.ID):
			st.OK = true
			st.Detail = "Ya instalado en " + emu.Name
		default:
			st.Detail = err.Error()
		}
		out = append(out, st)
	}
	return out
}

func firmwareInstalled(emu *bundle.Emulator, reqID string) bool {
	if emu == nil || emu.Firmware == nil || emu.Firmware.Requirement != reqID {
		return false
	}
	_, err := os.Stat(emu.Expand(emu.Firmware.Installed, nil))
	return err == nil
}

func (a *App) SetRequirementPath(id, path string) error {
	if library.RequirementByID(id) == nil {
		return fmt.Errorf("requisito desconocido: %s", id)
	}
	return a.store.SetPath(id, strings.TrimSpace(path))
}

// PickRequirementPath opens the right dialog (folder or file) for a
// requirement and saves the choice.
func (a *App) PickRequirementPath(id string) (string, error) {
	r := library.RequirementByID(id)
	if r == nil {
		return "", fmt.Errorf("requisito desconocido: %s", id)
	}
	var path string
	var err error
	if r.Kind == "dir" {
		path, err = runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: r.Label})
	} else {
		path, err = runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
			Title:   r.Label,
			Filters: []runtime.FileFilter{{DisplayName: "Firmware PS3 (*.PUP)", Pattern: "*.PUP;*.pup"}},
		})
	}
	if err != nil || path == "" {
		return "", err
	}
	return path, a.store.SetPath(id, path)
}

// ---- Launch preparation ----

// prepareLaunch validates everything a game needs and returns the command to
// run. For bundled emulators it also copies BIOS files where the emulator
// looks for them and installs PS3 firmware on first use.
func (a *App) prepareLaunch(g library.Game) ([]string, error) {
	c := library.ConsoleByID(g.Console)
	cfg := a.store.ConsoleConfig(g.Console)

	if _, err := os.Stat(g.Path); err != nil {
		return nil, fmt.Errorf("no se encuentra el juego: %s", g.Path)
	}

	var emu *bundle.Emulator
	if cfg.Emulator == "bundled" {
		emu = a.bundledFor(g.Console)
		if emu == nil {
			if st := a.GetBundleInfo(); st.Installing {
				return nil, errors.New("los emuladores incluidos aún se están preparando, espera un momento")
			}
			return nil, requirementError(g.Console, "no hay emulador incluido para %s; elige otro en Ajustes", c.ShortName)
		}
	} else if !commandAvailable(cfg.Command) {
		return nil, requirementError(g.Console, "no hay un emulador válido configurado para %s", c.ShortName)
	}

	paths := a.store.Settings().Paths
	for _, r := range library.RequirementsFor(g.Console) {
		files, err := r.Check(paths[r.ID])
		if err != nil {
			if firmwareInstalled(emu, r.ID) {
				continue
			}
			return nil, requirementError(g.Console, "%s: %s", r.Label, err)
		}
		if emu == nil {
			continue
		}
		if dst, ok := emu.Provision[r.ID]; ok {
			if err := provisionFiles(files, emu.Expand(dst, nil)); err != nil {
				return nil, fmt.Errorf("no se pudo copiar %s: %w", r.Label, err)
			}
		}
		if emu.Firmware != nil && emu.Firmware.Requirement == r.ID && !firmwareInstalled(emu, r.ID) {
			if err := a.installFirmware(emu, files[0]); err != nil {
				return nil, err
			}
		}
	}

	if emu == nil {
		return library.BuildCommand(cfg.Command, g.Path), nil
	}
	args := library.SplitCommand(emu.Command)
	for i, arg := range args {
		args[i] = emu.Expand(arg, map[string]string{"{rom}": g.Path})
	}
	return args, nil
}

// provisionFiles copies BIOS files into the emulator's BIOS folder, skipping
// ones already there with the same size.
func provisionFiles(files []string, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, src := range files {
		dst := filepath.Join(dir, filepath.Base(src))
		si, err := os.Stat(src)
		if err != nil {
			return err
		}
		if di, err := os.Stat(dst); err == nil && di.Size() == si.Size() {
			continue
		}
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// installFirmware runs the emulator's firmware installer and waits for it.
// RPCS3 shows its own progress dialog during this step.
func (a *App) installFirmware(emu *bundle.Emulator, pkg string) error {
	runtime.EventsEmit(a.ctx, "firmware:installing", emu.Name)
	args := library.SplitCommand(emu.Firmware.Install)
	for i, arg := range args {
		args[i] = emu.Expand(arg, map[string]string{"{file}": pkg})
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	err := cmd.Run()
	runtime.EventsEmit(a.ctx, "firmware:done", emu.Name)
	if !firmwareInstalled(emu, emu.Firmware.Requirement) {
		if err != nil {
			return fmt.Errorf("falló la instalación del firmware en %s: %w", emu.Name, err)
		}
		return fmt.Errorf("%s no terminó de instalar el firmware", emu.Name)
	}
	return nil
}

package main

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"emuhub/internal/update"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// version is this build's release, set by scripts/build-release.sh with
// -ldflags "-X main.version=v1.2.3". Other builds do not update themselves.
var version = "dev"

// releaseRepo is the GitHub repository releases and their emulators are
// published in.
var releaseRepo = "omarmendozaaa/emuhub"

// releaseBase replaces https://github.com when set at build time, for
// trying updates against a local server.
var releaseBase = ""

// How often a running EmuHub looks for a new version.
const updateInterval = 6 * time.Hour

type UpdateState struct {
	Current string `json:"current"`
	// Enabled is false for builds that are not releases.
	Enabled bool `json:"enabled"`
	// Available is the newer version found, if any, and Size its download.
	Available   string `json:"available"`
	Size        int64  `json:"size"`
	Checking    bool   `json:"checking"`
	Downloading bool   `json:"downloading"`
	Done        int64  `json:"done"`
	Total       int64  `json:"total"`
	Error       string `json:"error"`
	// Checked is when the last check finished, in Unix seconds.
	Checked int64 `json:"checked"`
	// Page is the web page of the newest release.
	Page string `json:"page"`
}

type updater struct {
	mu    sync.Mutex
	state UpdateState
	rel   *update.Release
	file  *update.File
}

func (a *App) updaterConfig() update.Updater {
	return update.Updater{Repo: releaseRepo, Current: version, Base: releaseBase}
}

func (a *App) GetUpdateState() UpdateState {
	a.update.mu.Lock()
	defer a.update.mu.Unlock()
	s := a.update.state
	u := a.updaterConfig()
	s.Current, s.Enabled, s.Page = version, u.Enabled(), u.Page()
	return s
}

func (a *App) setUpdate(fn func(*UpdateState)) {
	a.update.mu.Lock()
	fn(&a.update.state)
	a.update.mu.Unlock()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "update:state", a.GetUpdateState())
	}
}

// watchUpdates looks for a new version shortly after starting and then
// every few hours.
func (a *App) watchUpdates() {
	if !a.updaterConfig().Enabled() {
		return
	}
	time.Sleep(5 * time.Second)
	for {
		if err := a.CheckForUpdates(); err != nil {
			log.Printf("actualizaciones: %v", err)
		}
		time.Sleep(updateInterval)
	}
}

// CheckForUpdates looks for a newer release.
func (a *App) CheckForUpdates() error {
	u := a.updaterConfig()
	if !u.Enabled() {
		return errors.New("esta copia de EmuHub no es una versión publicada y no se actualiza sola")
	}
	a.update.mu.Lock()
	busy := a.update.state.Checking || a.update.state.Downloading
	a.update.mu.Unlock()
	if busy {
		return nil
	}
	a.setUpdate(func(s *UpdateState) { s.Checking, s.Error = true, "" })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rel, file, err := u.Latest(ctx)
	a.update.mu.Lock()
	a.update.rel, a.update.file = rel, file
	a.update.mu.Unlock()
	a.setUpdate(func(s *UpdateState) {
		s.Checking, s.Checked = false, time.Now().Unix()
		s.Available, s.Size = "", 0
		if err != nil {
			s.Error = err.Error()
			return
		}
		if file != nil {
			s.Available, s.Size = rel.Version, file.Size
		}
	})
	return err
}

// InstallUpdate downloads the version CheckForUpdates found, puts it in
// place of this one and restarts EmuHub with it. It refuses while a game or
// a room is open, since restarting would close them.
func (a *App) InstallUpdate() error {
	a.mu.Lock()
	busy := a.running != nil || a.remote.state.Role != ""
	a.mu.Unlock()
	if busy {
		return errors.New("cierra el juego o la sala antes de actualizar")
	}
	a.update.mu.Lock()
	rel, file := a.update.rel, a.update.file
	downloading := a.update.state.Downloading
	a.update.mu.Unlock()
	if downloading {
		return nil
	}
	if file == nil {
		return errors.New("no hay ninguna versión nueva")
	}

	a.setUpdate(func(s *UpdateState) { s.Downloading, s.Done, s.Total, s.Error = true, 0, file.Size, "" })
	last := int64(-1)
	exe, err := a.updaterConfig().Install(context.Background(), rel, file, func(done, total int64) {
		if pct := done * 100 / max(total, 1); pct != last {
			last = pct
			a.setUpdate(func(s *UpdateState) { s.Done, s.Total = done, total })
		}
	})
	if err == nil {
		err = update.Restart(exe)
		if err != nil {
			err = errors.New("EmuHub se actualizó, pero no se pudo volver a abrir: ábrelo tú")
		}
	}
	a.setUpdate(func(s *UpdateState) {
		s.Downloading = false
		if err != nil {
			s.Error = err.Error()
		}
	})
	if err != nil {
		return err
	}
	runtime.Quit(a.ctx)
	return nil
}

// OpenReleasePage opens the newest release in the browser, for downloading
// it by hand.
func (a *App) OpenReleasePage() {
	runtime.BrowserOpenURL(a.ctx, a.updaterConfig().Page())
}

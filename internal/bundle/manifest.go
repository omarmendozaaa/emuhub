// Package bundle installs and describes the emulators shipped inside a
// distributable EmuHub binary.
package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Lock is the build-time description of every pinned emulator release
// (emulators.lock.json).
type Lock struct {
	BundleVersion string      `json:"bundleVersion"`
	Emulators     []LockEntry `json:"emulators"`
}

type LockEntry struct {
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	Version   string               `json:"version"`
	Consoles  []string             `json:"consoles"`
	License   string               `json:"license"`
	Source    string               `json:"source"`
	Platforms map[string]*Platform `json:"platforms"`
}

// Platform is one downloadable build of an emulator plus how to run it.
type Platform struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Format string `json:"format"` // zip, 7z, tar.xz, appimage
	// Command template. {root} is the emulator's install folder, {rom} the
	// game path. Placeholders are expanded per argument, so paths with spaces
	// are safe.
	Command string `json:"command"`
	// Provision maps a requirement ID (e.g. "ps1.bios") to the folder the
	// emulator reads it from; validated files are copied there before launch.
	Provision map[string]string `json:"provision,omitempty"`
	Firmware  *Firmware         `json:"firmware,omitempty"`
}

// Firmware describes emulators that must install a firmware package once
// (RPCS3) rather than reading files from a folder.
type Firmware struct {
	Requirement string `json:"requirement"`
	Installed   string `json:"installed"` // path that exists once installed
	Install     string `json:"install"`   // command template, {file} is the package
}

// Manifest is written into the bundle and describes what it contains for a
// single platform.
type Manifest struct {
	BundleVersion string     `json:"bundleVersion"`
	Platform      string     `json:"platform"`
	Emulators     []Emulator `json:"emulators"`
}

type Emulator struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Consoles []string `json:"consoles"`
	License  string   `json:"license"`
	Source   string   `json:"source"`
	Platform
	// Root is filled in at runtime with the absolute install folder.
	Root string `json:"-"`
}

func ReadLock(path string) (*Lock, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l Lock
	return &l, json.Unmarshal(b, &l)
}

// CurrentPlatform returns the lock-file platform key for this build.
func CurrentPlatform() string {
	if runtime.GOOS == "darwin" {
		return "darwin-universal"
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

// Expand replaces path placeholders in a template argument.
func (e *Emulator) Expand(s string, extra map[string]string) string {
	home, _ := os.UserHomeDir()
	xdgData := os.Getenv("XDG_DATA_HOME")
	if xdgData == "" {
		xdgData = filepath.Join(home, ".local", "share")
	}
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		xdgConfig = filepath.Join(home, ".config")
	}
	pairs := []string{
		"{root}", e.Root,
		"{home}", home,
		"{xdgdata}", xdgData,
		"{xdgconfig}", xdgConfig,
		"{documents}", documentsDir(),
	}
	for k, v := range extra {
		pairs = append(pairs, k, v)
	}
	return filepath.FromSlash(strings.NewReplacer(pairs...).Replace(s))
}

// DataDir is where bundled emulators are installed: a per-user, non-roaming
// location (~/.local/share, %LocalAppData%, ~/Library/Application Support).
func DataDir() (string, error) {
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	default:
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".local", "share")
		}
	}
	if base == "" {
		var err error
		if base, err = os.UserCacheDir(); err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "emuhub"), nil
}

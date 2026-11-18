// Package update keeps EmuHub up to date from its GitHub releases.
//
// Every release publishes latest.json, which names the program file of each
// platform with its size and SHA-256. GitHub serves the newest release's
// copy at a fixed address, so checking for updates needs neither the GitHub
// API nor a server of our own.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"emuhub/internal/download"
)

// Release is latest.json.
type Release struct {
	Version string          `json:"version"`
	Files   map[string]File `json:"files"`
}

// File is one platform's program in a release.
type File struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Platform is this build's key in Release.Files.
func Platform() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// AssetName is the file a release publishes for a platform. The names carry
// no version, so releases/latest/download/<name> always links the newest.
func AssetName(platform string, complete bool) (string, bool) {
	names := map[string]string{
		"linux-amd64":   "linux-x86_64",
		"windows-amd64": "windows-x64.exe",
		"darwin-arm64":  "macos-arm64.zip",
		"darwin-amd64":  "macos-amd64.zip",
	}
	n, ok := names[platform]
	if !ok {
		return "", false
	}
	if complete {
		return "EmuHub-completo-" + n, true
	}
	return "EmuHub-" + n, true
}

// PlatformOf is the platform of a published program file, or "" for
// anything else, including the complete builds (updates always use the
// small ones).
func PlatformOf(name string) string {
	for _, p := range []string{"linux-amd64", "windows-amd64", "darwin-arm64", "darwin-amd64"} {
		if n, _ := AssetName(p, false); n == name {
			return p
		}
	}
	return ""
}

// ParseVersion reads a release version such as v1.2.3. Anything else, like
// a development build, is not a release.
func ParseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if !strings.HasPrefix(v, "v") || len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Newer reports whether release version a is newer than b.
func Newer(a, b string) bool {
	va, okA := ParseVersion(a)
	vb, okB := ParseVersion(b)
	if !okA || !okB {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

// Updater checks and installs releases of one GitHub repository.
type Updater struct {
	Repo    string // owner/name
	Current string // running version
	// Base replaces https://github.com, for tests.
	Base string
}

func (u Updater) base() string {
	if u.Base != "" {
		return u.Base
	}
	return "https://github.com"
}

// Enabled reports whether this build can update itself: only releases know
// their version.
func (u Updater) Enabled() bool {
	_, ok := ParseVersion(u.Current)
	return ok && u.Repo != ""
}

// Page is the newest release's web page.
func (u Updater) Page() string {
	return u.base() + "/" + u.Repo + "/releases/latest"
}

// Latest fetches the newest release and returns it when it is newer than the
// running version and has a program for this platform.
func (u Updater) Latest(ctx context.Context) (*Release, *File, error) {
	b, err := download.Get(ctx, u.base()+"/"+u.Repo+"/releases/latest/download/latest.json", 1<<20)
	if err != nil {
		return nil, nil, fmt.Errorf("no se pudo consultar si hay versiones nuevas: %w", err)
	}
	var r Release
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, nil, fmt.Errorf("la descripción de la última versión no es válida: %w", err)
	}
	if !Newer(r.Version, u.Current) {
		return &r, nil, nil
	}
	f, ok := r.Files[Platform()]
	if !ok {
		// Published without this platform yet (macOS is built by hand).
		return &r, nil, nil
	}
	return &r, &f, nil
}

// Install downloads f from release r and puts it in place of the running
// program. It returns the program to start for running the new version;
// see Restart.
func (u Updater) Install(ctx context.Context, r *Release, f *File, progress func(done, total int64)) (string, error) {
	if strings.ContainsAny(f.Name, `/\`) || f.Name == "" || strings.ContainsAny(r.Version, `/\`) {
		return "", errors.New("la versión publicada no es válida")
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	target, err := Target()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(target)
	tmp := filepath.Join(dir, ".emuhub-update-"+f.Name)
	url := u.base() + "/" + u.Repo + "/releases/download/" + r.Version + "/" + f.Name
	if err := download.File(ctx, url, tmp, f.Size, f.SHA256, progress); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return "", notWritable(dir)
		}
		return "", err
	}
	defer os.Remove(tmp)
	if err := replace(target, tmp); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return "", notWritable(dir)
		}
		return "", err
	}
	return exe, nil
}

func notWritable(dir string) error {
	return fmt.Errorf("EmuHub no puede escribir en %s; descarga la versión nueva a mano o mueve EmuHub a una carpeta tuya", dir)
}

// Target is what an update replaces: the program file, or on macOS the
// EmuHub.app that contains it.
func Target() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		// EmuHub.app/Contents/MacOS/emuhub
		if app := filepath.Dir(filepath.Dir(filepath.Dir(exe))); strings.HasSuffix(app, ".app") {
			return app, nil
		}
	}
	return exe, nil
}

// Cleanup removes what a previous update left behind: Windows cannot delete
// a running program, so the old one is set aside and removed on the next
// start.
func Cleanup() {
	if target, err := Target(); err == nil {
		os.RemoveAll(target + ".old")
	}
}

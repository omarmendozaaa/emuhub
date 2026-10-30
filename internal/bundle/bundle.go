package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// embedded holds the emulator archive in builds made with -tags bundle.
var embedded []byte

const (
	manifestName = "manifest.json"
	completeName = ".complete"
)

// Available reports whether this binary carries bundled emulators.
func Available() bool { return len(embedded) > 0 }

// Installed loads the manifest of an already-extracted bundle, if any.
func Installed(baseDir string) (*Manifest, bool) {
	m, err := Embedded()
	if err != nil {
		return nil, false
	}
	dir := versionDir(baseDir, m.BundleVersion)
	if _, err := os.Stat(filepath.Join(dir, completeName)); err != nil {
		return nil, false
	}
	m.setRoots(dir)
	return m, true
}

// Install extracts the embedded emulators into baseDir/emulators/<version>.
// Older bundle versions are removed afterwards.
func Install(baseDir string, progress func(done, total int64)) (*Manifest, error) {
	if !Available() {
		return nil, errors.New("este binario no incluye emuladores")
	}
	if m, ok := Installed(baseDir); ok {
		return m, nil
	}
	zr, err := zip.NewReader(bytes.NewReader(embedded), int64(len(embedded)))
	if err != nil {
		return nil, err
	}
	m, err := Embedded()
	if err != nil {
		return nil, err
	}

	final := versionDir(baseDir, m.BundleVersion)
	partial := final + ".partial"
	os.RemoveAll(partial)
	if err := os.MkdirAll(partial, 0o755); err != nil {
		return nil, err
	}
	if err := ExtractZip(zr, partial, progress); err != nil {
		os.RemoveAll(partial)
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(partial, completeName), []byte(m.BundleVersion), 0o644); err != nil {
		return nil, err
	}
	os.RemoveAll(final)
	if err := os.Rename(partial, final); err != nil {
		return nil, err
	}

	entries, _ := os.ReadDir(filepath.Join(baseDir, "emulators"))
	for _, e := range entries {
		if p := filepath.Join(baseDir, "emulators", e.Name()); p != final {
			os.RemoveAll(p)
		}
	}
	m.setRoots(final)
	return m, nil
}

func versionDir(baseDir, version string) string {
	return filepath.Join(baseDir, "emulators", version)
}

func (m *Manifest) setRoots(dir string) {
	for i := range m.Emulators {
		m.Emulators[i].Root = filepath.Join(dir, m.Emulators[i].ID)
	}
}

// ForConsole returns the bundled emulator for a console, if any.
func (m *Manifest) ForConsole(console string) *Emulator {
	if m == nil {
		return nil
	}
	for i := range m.Emulators {
		for _, c := range m.Emulators[i].Consoles {
			if c == console {
				return &m.Emulators[i]
			}
		}
	}
	return nil
}

// Embedded reads the manifest of the bundled emulators without installing
// them; Root is not set.
func Embedded() (*Manifest, error) {
	if !Available() {
		return nil, errors.New("sin paquete")
	}
	zr, err := zip.NewReader(bytes.NewReader(embedded), int64(len(embedded)))
	if err != nil {
		return nil, err
	}
	f, err := zr.Open(manifestName)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var m Manifest
	return &m, json.NewDecoder(f).Decode(&m)
}

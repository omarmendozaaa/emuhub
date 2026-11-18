package bundle

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"emuhub/internal/download"
)

// The emulators of a release build are described by data/emulators.json,
// embedded with -tags bundle. The archive itself is downloaded the first
// time it is needed, unless the binary also embeds it (-tags bundlezip).
var (
	embeddedDesc []byte
	embeddedZip  []byte
)

const (
	manifestName = "manifest.json"
	completeName = ".complete"
)

// Description is what a release build knows about its emulators: their
// manifest and the archive that holds them.
type Description struct {
	Manifest Manifest `json:"manifest"`
	Archive  Archive  `json:"archive"`
}

// Archive is the published zip with the emulators of one platform.
type Archive struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Phases of Install, reported with its progress.
const (
	PhaseDownload = "download"
	PhaseExtract  = "extract"
)

// Available reports whether this binary comes with bundled emulators, either
// inside it or to download.
func Available() bool { return len(embeddedDesc) > 0 }

// Included reports whether the emulators are inside this binary, so nothing
// needs downloading.
func Included() bool { return len(embeddedZip) > 0 }

func describe() (*Description, error) {
	if !Available() {
		return nil, errors.New("sin paquete")
	}
	var d Description
	if err := json.Unmarshal(embeddedDesc, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

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

// Install puts the emulators in baseDir/emulators/<version>, downloading
// their archive first when this binary does not include it. Older bundle
// versions are removed afterwards. progress receives the current phase and
// its progress in bytes.
func Install(ctx context.Context, baseDir string, progress func(phase string, done, total int64)) (*Manifest, error) {
	if m, ok := Installed(baseDir); ok {
		return m, nil
	}
	d, err := describe()
	if err != nil {
		return nil, errors.New("este binario no incluye emuladores")
	}
	m := &d.Manifest
	if progress == nil {
		progress = func(string, int64, int64) {}
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "emulators"), 0o755); err != nil {
		return nil, err
	}

	var zr *zip.Reader
	if Included() {
		if zr, err = zip.NewReader(bytes.NewReader(embeddedZip), int64(len(embeddedZip))); err != nil {
			return nil, err
		}
	} else {
		archive := versionDir(baseDir, m.BundleVersion) + ".zip"
		err := download.File(ctx, d.Archive.URL, archive, d.Archive.Size, d.Archive.SHA256, func(done, total int64) {
			progress(PhaseDownload, done, total)
		})
		if err != nil {
			return nil, err
		}
		defer os.Remove(archive)
		zf, err := zip.OpenReader(archive)
		if err != nil {
			return nil, err
		}
		defer zf.Close()
		zr = &zf.Reader
	}

	final := versionDir(baseDir, m.BundleVersion)
	partial := final + ".partial"
	os.RemoveAll(partial)
	if err := os.MkdirAll(partial, 0o755); err != nil {
		return nil, err
	}
	if err := ExtractZip(zr, partial, func(done, total int64) { progress(PhaseExtract, done, total) }); err != nil {
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
		if p := filepath.Join(baseDir, "emulators", e.Name()); p != final && p != final+".zip" {
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

// ByID returns a bundled program by its lock-file ID, if present.
func (m *Manifest) ByID(id string) *Emulator {
	if m == nil {
		return nil
	}
	for i := range m.Emulators {
		if m.Emulators[i].ID == id {
			return &m.Emulators[i]
		}
	}
	return nil
}

// Embedded returns the manifest of this binary's emulators without
// installing them; Root is not set.
func Embedded() (*Manifest, error) {
	d, err := describe()
	if err != nil {
		return nil, err
	}
	return &d.Manifest, nil
}

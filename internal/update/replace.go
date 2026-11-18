package update

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"emuhub/internal/bundle"
)

// replace puts the downloaded file new in place of target.
func replace(target, new string) error {
	switch {
	case runtime.GOOS == "darwin" && strings.HasSuffix(target, ".app"):
		return replaceApp(target, new)
	case runtime.GOOS == "darwin":
		return errors.New("esta copia de EmuHub no está dentro de EmuHub.app; descarga la versión nueva a mano")
	case runtime.GOOS == "windows":
		// A running program cannot be overwritten or deleted, but it can be
		// renamed; Cleanup deletes it on the next start.
		old := target + ".old"
		os.Remove(old)
		if err := os.Rename(target, old); err != nil {
			return err
		}
		if err := os.Rename(new, target); err != nil {
			os.Rename(old, target)
			return err
		}
		return nil
	default:
		// The running program keeps its open copy of the old file.
		if err := os.Chmod(new, 0o755); err != nil {
			return err
		}
		return os.Rename(new, target)
	}
}

// replaceApp swaps the EmuHub.app bundle for the one in the downloaded zip.
func replaceApp(target, archive string) error {
	stage := filepath.Join(filepath.Dir(target), ".emuhub-update-app")
	os.RemoveAll(stage)
	defer os.RemoveAll(stage)
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	err = bundle.ExtractZip(&zr.Reader, stage, nil)
	zr.Close()
	if err != nil {
		return err
	}
	apps, _ := filepath.Glob(filepath.Join(stage, "*.app"))
	if len(apps) != 1 {
		return errors.New("la versión descargada no contiene EmuHub.app")
	}
	old := target + ".old"
	os.RemoveAll(old)
	if err := os.Rename(target, old); err != nil {
		return err
	}
	if err := os.Rename(apps[0], target); err != nil {
		os.Rename(old, target)
		return err
	}
	os.RemoveAll(old)
	return nil
}

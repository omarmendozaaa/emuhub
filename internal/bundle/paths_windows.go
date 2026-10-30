package bundle

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// documentsDir follows the Documents known folder, which may be redirected
// (e.g. to OneDrive); DuckStation and PCSX2 use the same lookup.
func documentsDir() string {
	if p, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents")
}

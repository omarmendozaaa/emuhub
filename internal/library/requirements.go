package library

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Requirement is a system file a console needs that EmuHub cannot ship
// (BIOS dumps, firmware). The user drops it in the console's fixed folder
// (see BIOSDir) and it is validated before every launch.
type Requirement struct {
	ID      string `json:"id"`
	Console string `json:"console"`
	Label   string `json:"label"`
	Hint    string `json:"hint"`
	check   func(path string) ([]string, error)
}

var Requirements = []*Requirement{
	{
		ID: "ps1.bios", Console: "ps1", Label: "BIOS de PlayStation",
		Hint:  "Un volcado de BIOS de PS1 (p. ej. scph5501.bin, 512 KB) de tu propia consola.",
		check: checkDir(isPS1BIOS),
	},
	{
		ID: "ps2.bios", Console: "ps2", Label: "BIOS de PlayStation 2",
		Hint:  "Un volcado de BIOS de PS2 (4 MB) de tu propia consola.",
		check: checkDir(isPS2BIOS),
	},
	{
		ID: "ps3.firmware", Console: "ps3", Label: "Firmware de PlayStation 3",
		Hint:  "El archivo PS3UPDAT.PUP, descargable gratis desde la web oficial de PlayStation.",
		check: checkDir(isPS3Firmware),
	},
}

// BIOSDir is the fixed folder for a console's BIOS or firmware files:
// ~/ROMs/bios/<console>.
func BIOSDir(console string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "ROMs", "bios", console)
}

// EnsureBIOSDirs creates the folder of every console that needs system files,
// so the user only has to drop them in.
func EnsureBIOSDirs() error {
	for _, r := range Requirements {
		if err := os.MkdirAll(BIOSDir(r.Console), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func RequirementsFor(console string) []*Requirement {
	var out []*Requirement
	for _, r := range Requirements {
		if r.Console == console {
			out = append(out, r)
		}
	}
	return out
}

func RequirementByID(id string) *Requirement {
	for _, r := range Requirements {
		if r.ID == id {
			return r
		}
	}
	return nil
}

// Dir is the folder this requirement's files are expected in.
func (r *Requirement) Dir() string { return BIOSDir(r.Console) }

// Check validates a folder and returns the usable files found.
func (r *Requirement) Check(dir string) ([]string, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("no se encuentra la carpeta %s", dir)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s debe ser una carpeta", dir)
	}
	return r.check(dir)
}

func checkDir(valid func(path string, size int64) bool) func(string) ([]string, error) {
	return func(dir string) ([]string, error) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		var found []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if valid(p, info.Size()) {
				found = append(found, p)
			}
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("no hay ningún archivo válido en %s", dir)
		}
		sort.Strings(found)
		return found, nil
	}
}

// isPS1BIOS accepts 512 KiB images carrying Sony's copyright string, which
// every retail PS1 BIOS has near the start.
func isPS1BIOS(path string, size int64) bool {
	if size != 512*1024 {
		return false
	}
	head, err := readHead(path, 4096)
	return err == nil && bytes.Contains(head, []byte("Sony Computer Entertainment Inc"))
}

// isPS2BIOS accepts 4 MiB images whose ROMDIR lists RESET and ROMVER.
func isPS2BIOS(path string, size int64) bool {
	if size != 4*1024*1024 {
		return false
	}
	head, err := readHead(path, 64*1024)
	return err == nil && bytes.Contains(head, []byte("RESET\x00")) && bytes.Contains(head, []byte("ROMVER"))
}

// isPS3Firmware accepts .PUP files with the SCEUF header.
func isPS3Firmware(path string, _ int64) bool {
	if !strings.EqualFold(filepath.Ext(path), ".pup") {
		return false
	}
	head, err := readHead(path, 8)
	return err == nil && bytes.HasPrefix(head, []byte("SCEUF"))
}

func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	m, err := io.ReadFull(f, buf)
	if err == io.ErrUnexpectedEOF {
		err = nil
	}
	return buf[:m], err
}

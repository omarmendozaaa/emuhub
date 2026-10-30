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
// (BIOS dumps, firmware). The user points EmuHub at it and it is validated
// before every launch.
type Requirement struct {
	ID      string `json:"id"`
	Console string `json:"console"`
	Label   string `json:"label"`
	Kind    string `json:"kind"` // "dir" or "file"
	Hint    string `json:"hint"`
	check   func(path string) ([]string, error)
}

var Requirements = []*Requirement{
	{
		ID: "ps1.bios", Console: "ps1", Label: "BIOS de PlayStation", Kind: "dir",
		Hint:  "Carpeta con un volcado de BIOS de PS1 (p. ej. scph5501.bin, 512 KB) de tu propia consola.",
		check: checkBIOSDir(isPS1BIOS),
	},
	{
		ID: "ps2.bios", Console: "ps2", Label: "BIOS de PlayStation 2", Kind: "dir",
		Hint:  "Carpeta con un volcado de BIOS de PS2 (4 MB) de tu propia consola.",
		check: checkBIOSDir(isPS2BIOS),
	},
	{
		ID: "ps3.firmware", Console: "ps3", Label: "Firmware de PlayStation 3", Kind: "file",
		Hint:  "Archivo PS3UPDAT.PUP, descargable gratis desde la web oficial de PlayStation.",
		check: checkPUP,
	},
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

// Check validates the configured path and returns the usable files found.
func (r *Requirement) Check(path string) ([]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("no configurado")
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("no se encuentra %s", path)
	}
	if r.Kind == "dir" && !st.IsDir() {
		return nil, fmt.Errorf("debe ser una carpeta")
	}
	if r.Kind == "file" && st.IsDir() {
		return nil, fmt.Errorf("debe ser un archivo")
	}
	return r.check(path)
}

func checkBIOSDir(valid func(path string, size int64) bool) func(string) ([]string, error) {
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
			return nil, fmt.Errorf("no hay ninguna BIOS válida en la carpeta")
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

func checkPUP(path string) ([]string, error) {
	head, err := readHead(path, 8)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(head, []byte("SCEUF")) {
		return nil, fmt.Errorf("no es un archivo de firmware PUP válido")
	}
	return []string{path}, nil
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

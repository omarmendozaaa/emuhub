package library

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Console describes a supported platform: which files count as games and
// which emulators can run them.
type Console struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	ShortName   string   `json:"shortName"`
	Maker       string   `json:"maker"`
	Accent      string   `json:"accent"`
	Extensions  []string `json:"extensions"`
	LibretroDB  string   `json:"-"`
	candidates  []candidate
	folderGames bool // PS3 games are usually extracted folders, not single files
}

// candidate is an emulator we know how to invoke. The template uses {rom} as
// a placeholder for the game path.
type candidate struct {
	label    string
	bins     []string // executables to look up in PATH
	flatpak  string   // flatpak application id
	appimage string   // case-insensitive substring of an AppImage filename
	args     string
	core     string // libretro core name for RetroArch
}

var Consoles = []*Console{
	{
		ID: "snes", Name: "Super Nintendo", ShortName: "SNES", Maker: "Nintendo", Accent: "#7b5cff",
		Extensions: []string{".sfc", ".smc", ".fig", ".swc", ".bs", ".zip", ".7z"},
		LibretroDB: "Nintendo - Super Nintendo Entertainment System",
		candidates: []candidate{
			{label: "Snes9x", bins: []string{"snes9x-gtk", "snes9x"}, flatpak: "com.snes9x.Snes9x", args: "{rom}"},
			{label: "bsnes", bins: []string{"bsnes"}, flatpak: "dev.bsnes.bsnes", args: "--fullscreen {rom}"},
			{label: "RetroArch (Snes9x)", bins: []string{"retroarch"}, flatpak: "org.libretro.RetroArch", core: "snes9x", args: "-f {rom}"},
		},
	},
	{
		ID: "n64", Name: "Nintendo 64", ShortName: "N64", Maker: "Nintendo", Accent: "#21b35b",
		Extensions: []string{".z64", ".n64", ".v64", ".zip", ".7z"},
		LibretroDB: "Nintendo - Nintendo 64",
		candidates: []candidate{
			{label: "Rosalie's Mupen GUI", bins: []string{"RMG", "rmg"}, flatpak: "com.github.Rosalie241.RMG", appimage: "rmg", args: "--fullscreen --nogui {rom}"},
			{label: "simple64", bins: []string{"simple64-gui"}, flatpak: "io.github.simple64.simple64", args: "--nogui {rom}"},
			{label: "Mupen64Plus", bins: []string{"mupen64plus"}, args: "--fullscreen {rom}"},
			{label: "RetroArch (Mupen64Plus-Next)", bins: []string{"retroarch"}, flatpak: "org.libretro.RetroArch", core: "mupen64plus_next", args: "-f {rom}"},
		},
	},
	{
		ID: "ps1", Name: "PlayStation", ShortName: "PS1", Maker: "Sony", Accent: "#9aa4b5",
		Extensions: []string{".cue", ".chd", ".pbp", ".m3u", ".iso", ".img", ".ecm", ".bin"},
		LibretroDB: "Sony - PlayStation",
		candidates: []candidate{
			{label: "DuckStation", bins: []string{"duckstation-qt", "duckstation"}, flatpak: "org.duckstation.DuckStation", appimage: "duckstation", args: "-batch -fullscreen {rom}"},
			{label: "RetroArch (Beetle PSX HW)", bins: []string{"retroarch"}, flatpak: "org.libretro.RetroArch", core: "mednafen_psx_hw", args: "-f {rom}"},
			{label: "RetroArch (SwanStation)", bins: []string{"retroarch"}, flatpak: "org.libretro.RetroArch", core: "swanstation", args: "-f {rom}"},
		},
	},
	{
		ID: "ps2", Name: "PlayStation 2", ShortName: "PS2", Maker: "Sony", Accent: "#2f6bff",
		Extensions: []string{".iso", ".chd", ".cso", ".zso", ".gz", ".cue", ".bin", ".elf"},
		LibretroDB: "Sony - PlayStation 2",
		candidates: []candidate{
			{label: "PCSX2", bins: []string{"pcsx2-qt", "pcsx2"}, flatpak: "net.pcsx2.PCSX2", appimage: "pcsx2", args: "-batch -nogui -fullscreen -- {rom}"},
		},
	},
	{
		ID: "ps3", Name: "PlayStation 3", ShortName: "PS3", Maker: "Sony", Accent: "#e23b3b",
		Extensions:  []string{".iso"},
		LibretroDB:  "Sony - PlayStation 3",
		folderGames: true,
		candidates: []candidate{
			{label: "RPCS3", bins: []string{"rpcs3"}, flatpak: "net.rpcs3.RPCS3", appimage: "rpcs3", args: "--no-gui {rom}"},
		},
	},
}

func ConsoleByID(id string) *Console {
	for _, c := range Consoles {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// DetectedEmulator is an emulator found on this machine, ready to be used as
// a console's launch command.
type DetectedEmulator struct {
	Label   string `json:"label"`
	Command string `json:"command"`
}

// DetectEmulators lists every known emulator for the console that is
// installed, in order of preference.
func DetectEmulators(c *Console) []DetectedEmulator {
	var found []DetectedEmulator
	flatpaks := installedFlatpaks()
	appimages := findAppImages()

	for _, cand := range c.candidates {
		prefix := ""
		for _, b := range cand.bins {
			if p, err := exec.LookPath(b); err == nil {
				prefix = quote(p)
				break
			}
		}
		if prefix == "" && cand.appimage != "" {
			for _, a := range appimages {
				if strings.Contains(strings.ToLower(filepath.Base(a)), cand.appimage) {
					prefix = quote(a)
					break
				}
			}
		}
		if prefix == "" && cand.flatpak != "" && flatpaks[cand.flatpak] {
			prefix = "flatpak run " + cand.flatpak
		}
		if prefix == "" {
			continue
		}
		args := cand.args
		if cand.core != "" {
			core := findLibretroCore(cand.core)
			if core == "" {
				continue
			}
			args = "-L " + quote(core) + " " + args
		}
		found = append(found, DetectedEmulator{Label: cand.label, Command: prefix + " " + args})
	}
	return found
}

func installedFlatpaks() map[string]bool {
	out := map[string]bool{}
	if _, err := exec.LookPath("flatpak"); err != nil {
		return out
	}
	b, err := exec.Command("flatpak", "list", "--app", "--columns=application").Output()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out[line] = true
		}
	}
	return out
}

func findAppImages() []string {
	home, _ := os.UserHomeDir()
	var out []string
	for _, dir := range []string{"Applications", "AppImages", ".local/bin", "Downloads"} {
		matches, _ := filepath.Glob(filepath.Join(home, dir, "*.[Aa]pp[Ii]mage"))
		out = append(out, matches...)
	}
	return out
}

func findLibretroCore(name string) string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		"/usr/lib/libretro",
		"/usr/lib64/libretro",
		"/usr/lib/x86_64-linux-gnu/libretro",
		filepath.Join(home, ".config/retroarch/cores"),
		filepath.Join(home, ".var/app/org.libretro.RetroArch/config/retroarch/cores"),
	}
	for _, d := range dirs {
		p := filepath.Join(d, name+"_libretro.so")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func quote(s string) string {
	if strings.ContainsAny(s, " \t'\"") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// SplitCommand splits a command template into arguments, honouring single
// and double quotes and backslash escapes.
func SplitCommand(s string) []string {
	var args []string
	var cur strings.Builder
	inArg, inSingle, inDouble, escaped := false, false, false, false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && !inSingle:
			escaped = true
			inArg = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			inArg = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
			inArg = true
		case (r == ' ' || r == '\t') && !inSingle && !inDouble:
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args
}

// BuildCommand expands a launch template for a game path. If the template has
// no {rom} placeholder the path is appended as the last argument.
func BuildCommand(template, rom string) []string {
	args := SplitCommand(template)
	replaced := false
	for i, a := range args {
		if strings.Contains(a, "{rom}") {
			args[i] = strings.ReplaceAll(a, "{rom}", rom)
			replaced = true
		}
	}
	if !replaced && len(args) > 0 {
		args = append(args, rom)
	}
	return args
}

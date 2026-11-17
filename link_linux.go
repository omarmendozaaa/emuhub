package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// desktopFile is a hidden menu entry whose only job is to open emuhub://
// links, so it does not clash with an entry the user or a package created.
const desktopFile = "emuhub-link.desktop"

func registerScheme(exe string) error {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		data = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(data, "applications")
	path := filepath.Join(dir, desktopFile)
	content := desktopEntry(exe)
	if old, err := os.ReadFile(path); err == nil && string(old) == content && isDefaultHandler() {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	// Both are best effort: without them some desktops find the entry anyway.
	exec.Command("update-desktop-database", dir).Run()
	return exec.Command("xdg-mime", "default", desktopFile, "x-scheme-handler/emuhub").Run()
}

func isDefaultHandler() bool {
	out, err := exec.Command("xdg-mime", "query", "default", "x-scheme-handler/emuhub").Output()
	return err == nil && strings.TrimSpace(string(out)) == desktopFile
}

func desktopEntry(exe string) string {
	return "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=EmuHub\n" +
		"Comment=Abre los enlaces de sala de EmuHub\n" +
		"Exec=" + desktopQuote(exe) + " %u\n" +
		"Icon=emuhub\n" +
		"NoDisplay=true\n" +
		"Terminal=false\n" +
		"MimeType=x-scheme-handler/emuhub;\n"
}

// desktopQuote quotes a path for an Exec line, as the desktop entry
// specification asks for arguments with reserved characters.
func desktopQuote(s string) string {
	if !strings.ContainsAny(s, " \t\n\"'\\><~|&;$*?#()`%") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\\\`, `"`, `\\"`, "`", "\\\\`", `$`, `\\$`, `%`, `%%`)
	return `"` + r.Replace(s) + `"`
}

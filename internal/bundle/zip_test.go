package bundle

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestZipRoundTripKeepsModesAndSymlinks(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "app", "usr", "bin"), 0o755)
	os.WriteFile(filepath.Join(src, "app", "usr", "bin", "emu"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(src, "app", "readme.txt"), []byte("hola"), 0o644)
	if err := os.Symlink("usr/bin/emu", filepath.Join(src, "app", "AppRun")); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "b.zip")
	if err := WriteZip(src, out); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	dst := t.TempDir()
	if err := ExtractZip(&zr.Reader, dst, nil); err != nil {
		t.Fatal(err)
	}

	st, err := os.Stat(filepath.Join(dst, "app", "usr", "bin", "emu"))
	if err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("executable bit lost: %v %v", st.Mode(), err)
	}
	target, err := os.Readlink(filepath.Join(dst, "app", "AppRun"))
	if err != nil || target != "usr/bin/emu" {
		t.Fatalf("symlink lost: %q %v", target, err)
	}
}

func TestExpandPlaceholders(t *testing.T) {
	t.Setenv("HOME", "/home/x")
	t.Setenv("XDG_CONFIG_HOME", "")
	e := &Emulator{Root: "/opt/emu dir"}
	if got := e.Expand("{root}/AppRun", nil); got != "/opt/emu dir/AppRun" {
		t.Errorf("root: %s", got)
	}
	if got := e.Expand("{xdgconfig}/PCSX2/bios", nil); got != "/home/x/.config/PCSX2/bios" {
		t.Errorf("xdgconfig: %s", got)
	}
	if got := e.Expand("{rom}", map[string]string{"{rom}": "/g/a b.iso"}); got != "/g/a b.iso" {
		t.Errorf("rom: %s", got)
	}
}

package library

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSized(t *testing.T, path string, size int, marks map[int]string) {
	t.Helper()
	b := make([]byte, size)
	for off, s := range marks {
		copy(b[off:], s)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPS1BIOS(t *testing.T) {
	dir := t.TempDir()
	req := RequirementByID("ps1.bios")
	if _, err := req.Check(dir); err == nil {
		t.Fatal("empty folder accepted")
	}
	writeSized(t, filepath.Join(dir, "random.bin"), 512*1024, nil)
	writeSized(t, filepath.Join(dir, "small.bin"), 1024, map[int]string{0x108: "Sony Computer Entertainment Inc."})
	if _, err := req.Check(dir); err == nil {
		t.Fatal("invalid files accepted")
	}
	writeSized(t, filepath.Join(dir, "scph5501.bin"), 512*1024, map[int]string{0x108: "Sony Computer Entertainment Inc."})
	files, err := req.Check(dir)
	if err != nil || len(files) != 1 || filepath.Base(files[0]) != "scph5501.bin" {
		t.Fatalf("got %v, %v", files, err)
	}
	if _, err := req.Check(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing folder accepted")
	}
}

func TestPS2BIOS(t *testing.T) {
	dir := t.TempDir()
	req := RequirementByID("ps2.bios")
	writeSized(t, filepath.Join(dir, "bad.bin"), 4*1024*1024, map[int]string{0x2700: "RESET\x00"})
	if _, err := req.Check(dir); err == nil {
		t.Fatal("BIOS without ROMVER accepted")
	}
	writeSized(t, filepath.Join(dir, "SCPH-70012.bin"), 4*1024*1024, map[int]string{0x2700: "RESET\x00", 0x2750: "ROMVER"})
	if files, err := req.Check(dir); err != nil || len(files) != 1 {
		t.Fatalf("got %v, %v", files, err)
	}
}

func TestPS3Firmware(t *testing.T) {
	dir := t.TempDir()
	req := RequirementByID("ps3.firmware")
	writeSized(t, filepath.Join(dir, "other.PUP"), 64, nil)
	writeSized(t, filepath.Join(dir, "notes.txt"), 64, map[int]string{0: "SCEUF\x00\x00\x00"})
	if _, err := req.Check(dir); err == nil {
		t.Fatal("bad PUP accepted")
	}
	writeSized(t, filepath.Join(dir, "PS3UPDAT.PUP"), 64, map[int]string{0: "SCEUF\x00\x00\x00"})
	files, err := req.Check(dir)
	if err != nil || len(files) != 1 || filepath.Base(files[0]) != "PS3UPDAT.PUP" {
		t.Fatalf("got %v, %v", files, err)
	}
}

func TestScanSkipsBIOS(t *testing.T) {
	root := t.TempDir()
	sony := map[int]string{0x108: "Sony Computer Entertainment Inc."}
	for _, sub := range []string{"BIOS", "firmware", "system", "game"} {
		if err := os.Mkdir(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A dump in a system folder, even one the validator would reject.
	writeSized(t, filepath.Join(root, "BIOS", "SCPH1001.BIN"), 512*1024, sony)
	writeSized(t, filepath.Join(root, "firmware", "bad.bin"), 1024, nil)
	writeSized(t, filepath.Join(root, "system", "bad.bin"), 1024, nil)
	// A valid dump loose among the games.
	writeSized(t, filepath.Join(root, "scph5501.bin"), 512*1024, sony)
	writeSized(t, filepath.Join(root, "game", "Game.bin"), 1024, nil)

	games := Scan(ConsoleByID("ps1"), []string{root})
	if len(games) != 1 || games[0].Title != "Game" {
		t.Fatalf("got %+v", games)
	}
}

func TestBIOSDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := EnsureBIOSDirs(); err != nil {
		t.Fatal(err)
	}
	for _, console := range []string{"ps1", "ps2", "ps3"} {
		want := filepath.Join(home, "ROMs", "bios", console)
		if got := BIOSDir(console); got != want {
			t.Errorf("BIOSDir(%q) = %q, want %q", console, got, want)
		}
		if st, err := os.Stat(want); err != nil || !st.IsDir() {
			t.Errorf("%s not created: %v", want, err)
		}
	}
}

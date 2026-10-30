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
	if _, err := req.Check(""); err == nil {
		t.Fatal("empty path accepted")
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
	good := filepath.Join(dir, "PS3UPDAT.PUP")
	writeSized(t, good, 64, map[int]string{0: "SCEUF\x00\x00\x00"})
	bad := filepath.Join(dir, "other.PUP")
	writeSized(t, bad, 64, nil)
	if _, err := req.Check(good); err != nil {
		t.Fatal(err)
	}
	if _, err := req.Check(bad); err == nil {
		t.Fatal("bad PUP accepted")
	}
	if _, err := req.Check(dir); err == nil {
		t.Fatal("folder accepted as firmware file")
	}
}

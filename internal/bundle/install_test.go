package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// withDownloadedBundle makes this binary look like a release build whose
// emulators are published at a test server.
func withDownloadedBundle(t *testing.T, version string) (served *int) {
	t.Helper()
	stage := t.TempDir()
	m := Manifest{BundleVersion: version, Platform: CurrentPlatform(), Emulators: []Emulator{{ID: "emu", Name: "Emu", Consoles: []string{"snes"}}}}
	b, _ := json.Marshal(m)
	os.WriteFile(filepath.Join(stage, manifestName), b, 0o644)
	os.MkdirAll(filepath.Join(stage, "emu"), 0o755)
	os.WriteFile(filepath.Join(stage, "emu", "run"), []byte("#!/bin/sh\n"), 0o755)
	zipPath := filepath.Join(t.TempDir(), "emus.zip")
	if err := WriteZip(stage, zipPath); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(zipPath)
	h := sha256.Sum256(data)

	served = new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*served++
		w.Write(data)
	}))
	t.Cleanup(srv.Close)

	desc, _ := json.Marshal(Description{Manifest: m, Archive: Archive{URL: srv.URL, SHA256: hex.EncodeToString(h[:]), Size: int64(len(data))}})
	oldDesc, oldZip := embeddedDesc, embeddedZip
	embeddedDesc, embeddedZip = desc, nil
	t.Cleanup(func() { embeddedDesc, embeddedZip = oldDesc, oldZip })
	return served
}

func TestInstallDownloadsEmulators(t *testing.T) {
	base := t.TempDir()
	// Left over from an older bundle.
	os.MkdirAll(filepath.Join(base, "emulators", "old"), 0o755)
	served := withDownloadedBundle(t, "v2-test")

	if !Available() || Included() {
		t.Fatal("should be available to download, not included")
	}
	if _, ok := Installed(base); ok {
		t.Fatal("installed before installing")
	}
	phases := map[string]bool{}
	m, err := Install(context.Background(), base, func(phase string, done, total int64) { phases[phase] = true })
	if err != nil {
		t.Fatal(err)
	}
	if !phases[PhaseDownload] || !phases[PhaseExtract] {
		t.Fatalf("phases = %v", phases)
	}
	emu := m.ByID("emu")
	if emu == nil || emu.Root != filepath.Join(base, "emulators", "v2-test", "emu") {
		t.Fatalf("emu = %+v", emu)
	}
	if _, err := os.Stat(filepath.Join(emu.Root, "run")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(base, "emulators"))
	if len(entries) != 1 || entries[0].Name() != "v2-test" {
		t.Fatalf("emulators/ holds %v; the old bundle and the archive should be gone", entries)
	}
	if _, ok := Installed(base); !ok {
		t.Fatal("not installed after Install")
	}
	if _, err := Install(context.Background(), base, nil); err != nil || *served != 1 {
		t.Fatalf("second Install: err %v, downloads %d", err, *served)
	}
}

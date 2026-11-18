package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "v0.99.99", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v0.2.0", "dev", false},
		{"v0.2.0", "dev-abc123", false},
		{"v0.2.0-rc1", "v0.1.0", false},
		{"0.2.0", "v0.1.0", false},
		{"v0.02.0", "v0.1.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
	if (Updater{Repo: "a/b", Current: "dev"}).Enabled() {
		t.Error("a development build should not update itself")
	}
}

func TestAssetNames(t *testing.T) {
	for _, p := range []string{"linux-amd64", "windows-amd64", "darwin-arm64", "darwin-amd64"} {
		name, ok := AssetName(p, false)
		if !ok || PlatformOf(name) != p {
			t.Errorf("%s: %q does not map back", p, name)
		}
		full, _ := AssetName(p, true)
		if PlatformOf(full) != "" {
			t.Errorf("complete build %q must not be an update", full)
		}
	}
}

func TestLatest(t *testing.T) {
	rel := Release{Version: "v0.3.0", Files: map[string]File{
		Platform(): {Name: "EmuHub-x", SHA256: "00", Size: 10},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/o/r/releases/latest/download/latest.json" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(rel)
	}))
	defer srv.Close()

	u := Updater{Repo: "o/r", Current: "v0.2.0", Base: srv.URL}
	_, f, err := u.Latest(context.Background())
	if err != nil || f == nil || f.Name != "EmuHub-x" {
		t.Fatalf("Latest = %v, %v", f, err)
	}
	u.Current = "v0.3.0"
	if _, f, err := u.Latest(context.Background()); err != nil || f != nil {
		t.Fatalf("same version offered as update: %v, %v", f, err)
	}
	// A release without this platform yet is not an update.
	rel.Files = map[string]File{"plan9-amd64": {Name: "x"}}
	u.Current = "v0.2.0"
	if _, f, err := u.Latest(context.Background()); err != nil || f != nil {
		t.Fatalf("offered another platform's file: %v, %v", f, err)
	}
}

func TestReplace(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS replaces the whole .app")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "emuhub")
	next := filepath.Join(dir, ".emuhub-update-x")
	os.WriteFile(target, []byte("old"), 0o755)
	os.WriteFile(next, []byte("new"), 0o644)
	if err := replace(target, next); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(target)
	if string(b) != "new" {
		t.Fatalf("target = %q", b)
	}
	if st, _ := os.Stat(target); runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 {
		t.Fatal("new program is not executable")
	}
}

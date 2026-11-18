package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func serve(t *testing.T, data []byte) (*httptest.Server, *atomic.Int32, *atomic.Value) {
	var hits atomic.Int32
	var lastRange atomic.Value
	lastRange.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		lastRange.Store(r.Header.Get("Range"))
		http.ServeContent(w, r, "f", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits, &lastRange
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestFileResumesAndVerifies(t *testing.T) {
	data := bytes.Repeat([]byte("emuhub"), 100_000)
	srv, hits, lastRange := serve(t, data)
	dst := filepath.Join(t.TempDir(), "f.zip")

	// Half a download left over from before.
	if err := os.WriteFile(dst+".part", data[:250_000], 0o644); err != nil {
		t.Fatal(err)
	}
	var last int64
	if err := File(context.Background(), srv.URL, dst, int64(len(data)), "sha256:"+sum(data), func(d, _ int64) { last = d }); err != nil {
		t.Fatal(err)
	}
	if got := lastRange.Load().(string); got != "bytes=250000-" {
		t.Fatalf("Range = %q, want it to resume", got)
	}
	if last != int64(len(data)) {
		t.Fatalf("progress ended at %d", last)
	}
	b, _ := os.ReadFile(dst)
	if !bytes.Equal(b, data) {
		t.Fatal("downloaded file differs")
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatal(".part left behind")
	}

	// Already there: no new request.
	n := hits.Load()
	if err := File(context.Background(), srv.URL, dst, int64(len(data)), sum(data), nil); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != n {
		t.Fatal("downloaded a file that was already complete")
	}
}

func TestFileRejectsWrongChecksum(t *testing.T) {
	data := []byte("not what was published")
	srv, _, _ := serve(t, data)
	dst := filepath.Join(t.TempDir(), "f")
	err := File(context.Background(), srv.URL, dst, int64(len(data)), sum([]byte("other")), nil)
	if err == nil || !strings.Contains(err.Error(), "dañado") {
		t.Fatalf("err = %v", err)
	}
	for _, p := range []string{dst, dst + ".part"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist", p)
		}
	}
}

func TestFileNotFound(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	err := File(context.Background(), srv.URL, filepath.Join(t.TempDir(), "f"), 10, sum(nil), nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}
}

func TestFileStalls(t *testing.T) {
	old := stallTimeout
	stallTimeout = 200 * time.Millisecond
	defer func() { stallTimeout = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("abc"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	err := File(context.Background(), srv.URL, filepath.Join(t.TempDir(), "f"), 100, sum(nil), nil)
	if err != errStalled {
		t.Fatalf("err = %v, want stalled", err)
	}
}

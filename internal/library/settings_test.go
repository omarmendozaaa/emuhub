package library

import "testing"

func TestSplitServerKey(t *testing.T) {
	for _, c := range []struct{ server, key, wantServer, wantKey string }{
		{"https://a.workers.dev/?key=old", "", "https://a.workers.dev/", "old"},
		{"https://a.workers.dev/?key=old", "new", "https://a.workers.dev/", "new"},
		{" https://a.workers.dev ", " k ", "https://a.workers.dev", "k"},
		{"", "", "", ""},
	} {
		s, k := SplitServerKey(c.server, c.key)
		if s != c.wantServer || k != c.wantKey {
			t.Errorf("SplitServerKey(%q, %q) = %q, %q; want %q, %q", c.server, c.key, s, k, c.wantServer, c.wantKey)
		}
	}
}

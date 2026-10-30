package library

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

var bracketRe = regexp.MustCompile(`\s*\[[^\]]*\]`)

// thumbName applies the libretro-thumbnails file naming rules.
func thumbName(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("&*/:`<>?\\|\"", r) {
			return '_'
		}
		return r
	}, s)
}

// FetchCover downloads box art for the game from the libretro thumbnail
// server into dir and returns the saved file path.
func FetchCover(ctx context.Context, g Game, dir string) (string, error) {
	c := ConsoleByID(g.Console)
	if c == nil || c.LibretroDB == "" {
		return "", fmt.Errorf("consola sin base de portadas")
	}
	names := []string{g.FileName}
	if stripped := strings.TrimSpace(bracketRe.ReplaceAllString(g.FileName, "")); stripped != g.FileName {
		names = append(names, stripped)
	}
	if g.Title != g.FileName {
		names = append(names, g.Title)
	}

	var lastErr error
	for _, n := range names {
		u := "https://thumbnails.libretro.com/" + url.PathEscape(c.LibretroDB) + "/Named_Boxarts/" + url.PathEscape(thumbName(n)+".png")
		path, err := download(ctx, u, filepath.Join(dir, g.Console, g.ID+".png"))
		if err == nil {
			return path, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func download(ctx context.Context, u, dst string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "EmuHub/1.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("portada no encontrada (%d)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "image/") {
		return "", fmt.Errorf("respuesta no es imagen: %s", ct)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	f.Close()
	return dst, os.Rename(tmp, dst)
}

// CopyCover copies a user-chosen image into the data dir so it survives the
// original being moved.
func CopyCover(src, dir string, g Game) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	dst := filepath.Join(dir, g.Console, g.ID+"-custom"+strings.ToLower(filepath.Ext(src)))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return dst, err
}

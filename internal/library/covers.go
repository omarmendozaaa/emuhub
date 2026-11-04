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
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

var thumbnailsBase = "https://thumbnails.libretro.com/"

// boxartIndexes caches each console's list of box art files for this session.
var (
	boxartIndexMu sync.Mutex
	boxartIndexes = map[string][]string{}
)

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
		path, err := download(ctx, boxartURL(c, thumbName(n)), filepath.Join(dir, g.Console, g.ID+".png"))
		if err == nil {
			return path, nil
		}
		lastErr = err
	}

	// Most ROMs are not named exactly like their box art, which usually carries
	// a region: "Bloody Roar II" is "Bloody Roar II (USA)".
	index, err := boxartIndex(ctx, c)
	if err != nil {
		return "", err
	}
	if name := bestBoxart(index, g.Title); name != "" {
		return download(ctx, boxartURL(c, name), filepath.Join(dir, g.Console, g.ID+".png"))
	}
	return "", lastErr
}

func boxartURL(c *Console, name string) string {
	return thumbnailsBase + url.PathEscape(c.LibretroDB) + "/Named_Boxarts/" + url.PathEscape(name+".png")
}

var boxartHrefRe = regexp.MustCompile(`href="([^"?/]+)\.png"`)

// boxartIndex lists the box art names available for a console.
func boxartIndex(ctx context.Context, c *Console) ([]string, error) {
	boxartIndexMu.Lock()
	defer boxartIndexMu.Unlock()
	if index, ok := boxartIndexes[c.ID]; ok {
		return index, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, thumbnailsBase+url.PathEscape(c.LibretroDB)+"/Named_Boxarts/", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "EmuHub/1.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("índice de portadas no disponible (%d)", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var index []string
	for _, m := range boxartHrefRe.FindAllSubmatch(body, -1) {
		if name, err := url.PathUnescape(string(m[1])); err == nil {
			index = append(index, name)
		}
	}
	boxartIndexes[c.ID] = index
	return index, nil
}

var (
	titleSymbolsRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	nonRetailRe    = regexp.MustCompile(`(?i)\((demo|beta|proto|prototype|sample|unl|pirate|aftermarket)[^)]*\)`)
	boxartRegions  = []string{"USA", "World", "Europe", "Japan"}
)

// titleKey reduces a title to what must match: case, punctuation and tags such
// as the region do not count, and neither does the article, which box art names
// move to the end ("Legend of Zelda, The").
func titleKey(s string) string {
	s = strings.ToLower(tagRe.ReplaceAllString(s, ""))
	s = strings.TrimPrefix(s, "the ")
	s = strings.ReplaceAll(s, ", the", "")
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, titleSymbolsRe.ReplaceAllString(s, ""))
}

// bestBoxart picks the retail box art for a title, preferring the most common
// regions. Only exact titles count: no cover is better than another game's.
func bestBoxart(index []string, title string) string {
	key := titleKey(title)
	if key == "" {
		return ""
	}
	var found []string
	for _, name := range index {
		if titleKey(name) == key && !nonRetailRe.MatchString(name) {
			found = append(found, name)
		}
	}
	rank := func(name string) int {
		for i, region := range boxartRegions {
			if strings.Contains(name, region) {
				return i
			}
		}
		return len(boxartRegions)
	}
	sort.Slice(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if len(a) != len(b) {
			return len(a) < len(b)
		}
		return a < b
	})
	if len(found) == 0 {
		return ""
	}
	return found[0]
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

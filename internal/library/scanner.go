package library

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxScanDepth = 6

var imageExts = []string{".png", ".jpg", ".jpeg", ".webp"}

// systemDirs are folder names that hold BIOS or firmware files, never games.
var systemDirs = map[string]bool{"bios": true, "firmware": true, "system": true}

func gameID(console, path string) string {
	h := sha1.Sum([]byte(console + "\x00" + path))
	return hex.EncodeToString(h[:8])
}

var tagRe = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)

// CleanTitle turns a No-Intro/Redump file name like
// "Super Mario World (USA) [!]" into "Super Mario World".
func CleanTitle(name string) string {
	t := strings.Join(strings.Fields(tagRe.ReplaceAllString(name, "")), " ")
	t = strings.ReplaceAll(t, "_", " ")
	if t == "" {
		return name
	}
	return t
}

// Scan walks the console's ROM folders and returns the games found.
func Scan(c *Console, dirs []string) []*Game {
	var games []*Game
	seen := map[string]bool{}
	add := func(g *Game) {
		if seen[g.Path] {
			return
		}
		seen[g.Path] = true
		g.ID = gameID(c.ID, g.Path)
		g.Console = c.ID
		g.AddedAt = time.Now().Unix()
		games = append(games, g)
	}

	exts := map[string]bool{}
	for _, e := range c.Extensions {
		exts[e] = true
	}

	for _, root := range dirs {
		root = filepath.Clean(root)
		rootDepth := strings.Count(root, string(os.PathSeparator))
		skip := map[string]bool{} // files referenced by .cue/.m3u in their directory

		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if strings.Count(path, string(os.PathSeparator))-rootDepth > maxScanDepth {
					return filepath.SkipDir
				}
				if path != root && (strings.HasPrefix(d.Name(), ".") || systemDirs[strings.ToLower(d.Name())]) {
					return filepath.SkipDir
				}
				if c.folderGames {
					if g := ps3FolderGame(path); g != nil {
						add(g)
						return filepath.SkipDir
					}
				}
				markReferenced(path, skip)
				return nil
			}

			ext := strings.ToLower(filepath.Ext(path))
			if !exts[ext] || skip[path] || (ext == ".bin" && isBIOSFile(path, d)) {
				return nil
			}
			base := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
			add(&Game{
				Title:    CleanTitle(base),
				FileName: base,
				Path:     path,
				Cover:    siblingImage(root, path, base),
			})
			return nil
		})
	}
	return games
}

// isBIOSFile catches BIOS dumps left among the games, wherever they are.
func isBIOSFile(path string, d fs.DirEntry) bool {
	info, err := d.Info()
	if err != nil {
		return false
	}
	return isPS1BIOS(path, info.Size()) || isPS2BIOS(path, info.Size())
}

// markReferenced records the tracks listed in .cue sheets and the discs
// listed in .m3u playlists of a directory so they are not added twice.
// A loose .bin is skipped whenever its directory has any .cue.
func markReferenced(dir string, skip map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	hasCue := false
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".cue" {
			hasCue = true
		}
		if ext != ".m3u" && ext != ".cue" {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			ref := ""
			if ext == ".m3u" && line != "" && !strings.HasPrefix(line, "#") {
				ref = line
			} else if ext == ".cue" && strings.HasPrefix(strings.ToUpper(line), "FILE ") {
				if i, j := strings.Index(line, `"`), strings.LastIndex(line, `"`); j > i {
					ref = line[i+1 : j]
				}
			}
			if ref != "" {
				skip[filepath.Join(dir, ref)] = true
			}
		}
		f.Close()
	}
	if hasCue {
		for _, e := range entries {
			if strings.EqualFold(filepath.Ext(e.Name()), ".bin") {
				skip[filepath.Join(dir, e.Name())] = true
			}
		}
	}
}

func siblingImage(root, rom, base string) string {
	dir := filepath.Dir(rom)
	for _, d := range []string{dir, filepath.Join(dir, "covers"), filepath.Join(root, "covers"), filepath.Join(root, "boxart")} {
		for _, ext := range imageExts {
			p := filepath.Join(d, base+ext)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// ps3FolderGame recognises an extracted PS3 disc (GAME/PS3_GAME/...) or an
// installed HDD game (BLUS12345/USRDIR/EBOOT.BIN). Updates and DLC are
// ignored.
func ps3FolderGame(dir string) *Game {
	content := dir
	if _, err := os.Stat(filepath.Join(dir, "PS3_GAME", "PARAM.SFO")); err == nil {
		content = filepath.Join(dir, "PS3_GAME")
	}
	eboot := filepath.Join(content, "USRDIR", "EBOOT.BIN")
	if _, err := os.Stat(eboot); err != nil {
		return nil
	}
	sfo, err := ReadSFO(filepath.Join(content, "PARAM.SFO"))
	if err != nil {
		return nil
	}
	switch sfo["CATEGORY"] {
	case "GD", "AT", "2D", "CB", "SD", "AP", "AM", "AV", "WT":
		return nil
	}
	title := strings.Join(strings.Fields(sfo["TITLE"]), " ")
	if title == "" {
		title = filepath.Base(dir)
	}
	g := &Game{Title: title, FileName: title, Path: eboot, Serial: sfo["TITLE_ID"]}
	if p := filepath.Join(content, "ICON0.PNG"); fileExists(p) {
		g.Cover = p
	}
	if p := filepath.Join(content, "PIC1.PNG"); fileExists(p) {
		g.Backdrop = p
	}
	return g
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

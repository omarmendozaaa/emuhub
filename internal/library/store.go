package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Game struct {
	ID          string `json:"id"`
	Console     string `json:"console"`
	Title       string `json:"title"`
	FileName    string `json:"fileName"` // base name without extension, used for box art lookup
	Path        string `json:"path"`
	Serial      string `json:"serial,omitempty"`
	Cover       string `json:"-"`
	Backdrop    string `json:"-"`
	HasCover    bool   `json:"hasCover"`
	HasBackdrop bool   `json:"hasBackdrop"`
	CoverRev    int64  `json:"coverRev"` // changes whenever the cover image does, for cache busting
	Favorite    bool   `json:"favorite"`
	AddedAt     int64  `json:"addedAt"`
	LastPlayed  int64  `json:"lastPlayed"`
	PlaySeconds int64  `json:"playSeconds"`
}

// gameRecord is the on-disk form of Game; cover paths are persisted but not
// exposed to the frontend, which loads images through /media/.
type gameRecord struct {
	Game
	Cover    string `json:"cover,omitempty"`
	Backdrop string `json:"backdrop,omitempty"`
}

type ConsoleConfig struct {
	// Emulator is "bundled" to use the emulator shipped with EmuHub, or empty
	// to run Command.
	Emulator string   `json:"emulator"`
	Command  string   `json:"command"`
	RomDirs  []string `json:"romDirs"`
}

type Settings struct {
	MinimizeOnLaunch bool              `json:"minimizeOnLaunch"`
	AutoCovers       bool              `json:"autoCovers"`
	Paths            map[string]string `json:"paths"` // requirement ID -> BIOS folder / firmware file
}

type fileData struct {
	Version  int                       `json:"version"`
	Settings Settings                  `json:"settings"`
	Consoles map[string]*ConsoleConfig `json:"consoles"`
	Games    []gameRecord              `json:"games"`
}

// Store persists the library as a JSON file in the user's config directory.
type Store struct {
	mu       sync.RWMutex
	path     string
	dir      string
	settings Settings
	consoles map[string]*ConsoleConfig
	games    map[string]*Game
}

func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "emuhub"), nil
}

func OpenStore() (*Store, error) {
	dir, err := DataDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "covers"), 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		path:     filepath.Join(dir, "library.json"),
		dir:      dir,
		settings: Settings{MinimizeOnLaunch: true, AutoCovers: true},
		consoles: map[string]*ConsoleConfig{},
		games:    map[string]*Game{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	for _, c := range Consoles {
		if s.consoles[c.ID] == nil {
			s.consoles[c.ID] = &ConsoleConfig{RomDirs: []string{}}
		}
	}
	return s, nil
}

func (s *Store) Dir() string { return s.dir }

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var d fileData
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	s.settings = d.Settings
	if d.Consoles != nil {
		s.consoles = d.Consoles
	}
	for _, r := range d.Games {
		g := r.Game
		g.Cover, g.Backdrop = r.Cover, r.Backdrop
		g.HasCover, g.HasBackdrop = g.Cover != "", g.Backdrop != ""
		s.games[g.ID] = &g
	}
	return nil
}

// saveLocked writes the library atomically. Caller must hold s.mu.
func (s *Store) saveLocked() error {
	d := fileData{Version: 1, Settings: s.settings, Consoles: s.consoles}
	for _, g := range s.games {
		d.Games = append(d.Games, gameRecord{Game: *g, Cover: g.Cover, Backdrop: g.Backdrop})
	}
	sort.Slice(d.Games, func(i, j int) bool { return d.Games[i].ID < d.Games[j].ID })
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Games() []Game {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Game, 0, len(s.games))
	for _, g := range s.games {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

func (s *Store) Game(id string) (Game, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.games[id]
	if !ok {
		return Game{}, false
	}
	return *g, true
}

// UpdateGame applies fn to the stored game and persists the change.
func (s *Store) UpdateGame(id string, fn func(*Game)) (Game, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.games[id]
	if !ok {
		return Game{}, errors.New("juego no encontrado")
	}
	before := g.Cover
	fn(g)
	if g.Cover != before {
		g.CoverRev = time.Now().UnixNano()
	}
	g.HasCover, g.HasBackdrop = g.Cover != "", g.Backdrop != ""
	return *g, s.saveLocked()
}

func (s *Store) RemoveGame(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.games, id)
	return s.saveLocked()
}

// ReplaceConsoleGames swaps the games of one console for a fresh scan result,
// keeping user data (favorites, playtime, custom covers) for games that are
// still present.
func (s *Store) ReplaceConsoleGames(console string, scanned []*Game) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	for _, g := range scanned {
		seen[g.ID] = true
		if old, ok := s.games[g.ID]; ok {
			g.Favorite, g.AddedAt = old.Favorite, old.AddedAt
			g.LastPlayed, g.PlaySeconds = old.LastPlayed, old.PlaySeconds
			g.CoverRev = old.CoverRev
			// Covers downloaded or chosen by the user live in the data dir and
			// win over whatever the scan found next to the game.
			if g.Cover == "" || (old.Cover != "" && strings.HasPrefix(old.Cover, s.dir)) {
				g.Cover = old.Cover
			}
			if g.Backdrop == "" {
				g.Backdrop = old.Backdrop
			}
		}
		g.HasCover, g.HasBackdrop = g.Cover != "", g.Backdrop != ""
		s.games[g.ID] = g
	}
	for id, g := range s.games {
		if g.Console == console && !seen[id] {
			delete(s.games, id)
		}
	}
	return s.saveLocked()
}

func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.settings
	out.Paths = map[string]string{}
	for k, v := range s.settings.Paths {
		out.Paths[k] = v
	}
	return out
}

func (s *Store) SetPath(id, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings.Paths == nil {
		s.settings.Paths = map[string]string{}
	}
	s.settings.Paths[id] = path
	return s.saveLocked()
}

func (s *Store) SetSettings(v Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = v
	return s.saveLocked()
}

func (s *Store) ConsoleConfig(id string) ConsoleConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.consoles[id]
	if c == nil {
		return ConsoleConfig{RomDirs: []string{}}
	}
	return ConsoleConfig{Emulator: c.Emulator, Command: c.Command, RomDirs: append([]string{}, c.RomDirs...)}
}

func (s *Store) SetConsoleConfig(id string, cfg ConsoleConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.RomDirs == nil {
		cfg.RomDirs = []string{}
	}
	s.consoles[id] = &cfg
	return s.saveLocked()
}

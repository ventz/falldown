package game

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Settings are the values the original kept inside the program itself,
// which MirageOS wrote back to archive on exit: high score, speed, and
// player count.
type Settings struct {
	High    uint16 `json:"high"`
	Speed   uint8  `json:"speed"`
	Players uint8  `json:"players"`
}

// Store persists Settings per game version.
type Store interface {
	Load(version string) (Settings, bool)
	Save(version string, s Settings)
}

// FileStore keeps settings in a small JSON file.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// DefaultStorePath is $XDG_CONFIG_HOME/falldown/settings.json (or the
// platform's config directory).
func DefaultStorePath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		if d, err := os.UserConfigDir(); err == nil {
			dir = d
		} else {
			dir = "."
		}
	}
	return filepath.Join(dir, "falldown", "settings.json")
}

// NewFileStore returns a store backed by path.
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

// read loads every version's settings. A file that doesn't parse is moved
// aside to .bad, so the next save can't silently drop the other version's
// high score from it.
func (f *FileStore) read() map[string]Settings {
	all := map[string]Settings{}
	b, err := os.ReadFile(f.path)
	if err != nil {
		return all
	}
	if err := json.Unmarshal(b, &all); err != nil {
		log.Printf("settings: %s is unreadable (%v); moving it to .bad", f.path, err)
		os.Rename(f.path, f.path+".bad")
		return map[string]Settings{}
	}
	return all
}

// Load returns the saved settings for version, if any.
func (f *FileStore) Load(version string) (Settings, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.read()[version]
	return s, ok
}

// Save writes settings for version. Errors are ignored: losing a high
// score is better than crashing a game.
func (f *FileStore) Save(version string, s Settings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.read()
	all[version] = s
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(f.path), 0o755)
	tmp := f.path + ".tmp"
	if os.WriteFile(tmp, append(b, '\n'), 0o644) == nil {
		os.Rename(tmp, f.path)
	}
}

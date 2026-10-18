package game

import (
	"encoding/json"
	"syscall/js"
)

const storageKey = "falldown-settings"

// DefaultStore keeps settings in the browser's localStorage, in the same
// JSON shape as the desktop settings file.
func DefaultStore() Store { return localStore{} }

type localStore struct{}

func (localStore) read() map[string]Settings {
	all := map[string]Settings{}
	ls := js.Global().Get("localStorage")
	if ls.IsUndefined() || ls.IsNull() {
		return all
	}
	if v := ls.Call("getItem", storageKey); v.Type() == js.TypeString {
		json.Unmarshal([]byte(v.String()), &all)
	}
	return all
}

func (s localStore) Load(version string) (Settings, bool) {
	v, ok := s.read()[version]
	return v, ok
}

// Save ignores failures (private browsing, storage disabled), as the file
// store does: losing a high score is better than stopping the game.
func (s localStore) Save(version string, set Settings) {
	all := s.read()
	all[version] = set
	b, err := json.Marshal(all)
	if err != nil {
		return
	}
	defer func() { recover() }() // setItem throws when storage is full or blocked
	if ls := js.Global().Get("localStorage"); !ls.IsUndefined() && !ls.IsNull() {
		ls.Call("setItem", storageKey, string(b))
	}
}

package game

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "falldown", "settings.json")
	f := NewFileStore(path)
	if _, ok := f.Load("1.65"); ok {
		t.Fatal("empty store returned settings")
	}
	f.Save("1.5", Settings{High: 120, Speed: 1, Players: 1})
	f.Save("1.65", Settings{High: 900, Speed: 2, Players: 2})

	g := NewFileStore(path)
	if s, ok := g.Load("1.5"); !ok || s.High != 120 {
		t.Fatalf("1.5: got %+v, %v", s, ok)
	}
	if s, ok := g.Load("1.65"); !ok || s != (Settings{High: 900, Speed: 2, Players: 2}) {
		t.Fatalf("1.65: got %+v, %v", s, ok)
	}
}

// A corrupt file is kept as .bad rather than overwritten by the next save.
func TestFileStoreCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := NewFileStore(path)
	if _, ok := f.Load("1.65"); ok {
		t.Fatal("corrupt store returned settings")
	}
	if b, err := os.ReadFile(path + ".bad"); err != nil || string(b) != "{not json" {
		t.Fatalf("corrupt file not kept as .bad: %q, %v", b, err)
	}
	f.Save("1.65", Settings{High: 5, Speed: 1, Players: 1})
	if s, ok := f.Load("1.65"); !ok || s.High != 5 {
		t.Fatalf("save after corrupt file: got %+v, %v", s, ok)
	}
}

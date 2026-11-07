// notes: the smallest real paddock extension, and the one to copy to start a new one.
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/getshepherd/paddock"
)

//go:embed web
var web embed.FS

type Note struct {
	ID   int       `json:"id"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

func main() {
	app := paddock.New("notes", "Short notes kept on this machine", web)
	s := &store{path: filepath.Join(app.Dir(), "notes.json")}

	paddock.Command(app, "list", "List the notes, oldest first", func(_ *paddock.Ctx, _ struct{}) ([]Note, error) {
		return s.load()
	})
	paddock.Command(app, "add", "Add a note", func(c *paddock.Ctx, in struct {
		Text string `json:"text" help:"the note's text"`
	}) (Note, error) {
		if in.Text == "" {
			return Note{}, errors.New("text is empty")
		}
		var n Note
		err := s.change(func(notes []Note) []Note {
			n = Note{ID: int(time.Now().UnixMilli()), Text: in.Text, At: time.Now()}
			return append(notes, n)
		})
		if err == nil {
			c.Emit("changed", n)
		}
		return n, err
	})
	paddock.Command(app, "remove", "Remove a note", func(c *paddock.Ctx, in struct {
		ID int `json:"id" help:"the note's id, from list"`
	}) (int, error) {
		found := false
		err := s.change(func(notes []Note) []Note {
			return slices.DeleteFunc(notes, func(n Note) bool { found = found || n.ID == in.ID; return n.ID == in.ID })
		})
		if err == nil && !found {
			err = fmt.Errorf("no note %d", in.ID)
		}
		if err == nil {
			c.Emit("changed", in.ID)
		}
		return in.ID, err
	})

	app.Run()
}

// store keeps the notes in one JSON file. ponytail: the whole file is read and written on each change; SQLite once
// there are thousands.
type store struct {
	path string
	mu   sync.Mutex
}

func (s *store) load() ([]Note, error) {
	notes := []Note{}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return notes, nil
	}
	if err != nil {
		return nil, err
	}
	return notes, json.Unmarshal(b, &notes)
}

func (s *store) change(fn func([]Note) []Note) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	notes, err := s.load()
	if err != nil {
		return err
	}
	b, err := json.Marshal(fn(notes))
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}

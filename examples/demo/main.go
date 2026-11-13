// demo: an example of a Shepherd web page built on paddock, the one to copy to start a new one. Notes show commands, events and the CLI;
// files and open show a page reaching the machine; the page also shows every element paddock.css dresses.
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	paddock "github.com/getshepherd/extensions"
)

//go:embed web
var web embed.FS

// What agents should know beyond the commands: `demo skill` prints it before them.
//
//go:embed skill.md
var skill string

type Note struct {
	ID   int       `json:"id"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

func main() {
	app := paddock.New("demo", "An example Shepherd web page: notes, this machine's files, every UI element", web)
	app.Skill(skill)
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

	// The machine itself: what a page alone can't reach, a paddock command can.
	paddock.Command(app, "files", "List a folder (the home folder if no path)", func(_ *paddock.Ctx, in struct {
		Path string `json:"path" help:"the folder"`
	}) (Folder, error) {
		dir := in.Path
		if dir == "" {
			var err error
			if dir, err = os.UserHomeDir(); err != nil {
				return Folder{}, err
			}
		}
		dir, err := filepath.Abs(dir)
		if err != nil {
			return Folder{}, err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return Folder{}, err
		}
		f := Folder{Path: dir, Parent: filepath.Dir(dir), Files: []File{}}
		for _, e := range entries {
			info, err := e.Info()
			// ponytail: dotfiles hidden; a flag for them when someone asks.
			if err != nil || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			f.Files = append(f.Files, File{Name: e.Name(), Path: filepath.Join(dir, e.Name()), Dir: e.IsDir(), Size: info.Size()})
		}
		return f, nil
	})
	paddock.Command(app, "open", "Open a file or folder in its default app", func(_ *paddock.Ctx, in struct {
		Path string `json:"path" help:"the file or folder"`
	}) (string, error) {
		// Only things on disk: `open` also takes URLs and app names.
		if _, err := os.Stat(in.Path); err != nil {
			return "", err
		}
		opener := "xdg-open"
		if runtime.GOOS == "darwin" {
			opener = "open"
		}
		return in.Path, exec.Command(opener, in.Path).Run()
	})

	app.Run()
}

type Folder struct {
	Path   string `json:"path"`
	Parent string `json:"parent"`
	Files  []File `json:"files"`
}

type File struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
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

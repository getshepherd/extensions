package paddock

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed ui
var ui embed.FS

// state is what the service, the CLI and Shepherd share: where the server listens, the token that lets one in, and
// what Shepherd lists: the name, the description, and when it runs (startLogin or startShepherd).
type state struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Port        int    `json:"port"`
	Token       string `json:"token"`
	Start       string `json:"start"`
}

// When an extension runs: as a service from login on, or while Shepherd runs (Shepherd starts `<binary> serve`).
const (
	startLogin    = "login"
	startShepherd = "shepherd"
)

func (s state) url() string { return fmt.Sprintf("http://127.0.0.1:%d/?token=%s", s.Port, s.Token) }

func (a *App) statePath() string { return filepath.Join(a.Dir(), "state.json") }

func (a *App) loadState() (state, error) {
	var s state
	b, err := os.ReadFile(a.statePath())
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	return s, err
}

func (a *App) saveState(s state) error {
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(a.statePath(), b, 0o600)
}

// ensureState keeps the port and token picked before, so the URL saved in Shepherd keeps working; port, if not 0,
// replaces the port. The name and description follow the binary.
func (a *App) ensureState(port int) (state, error) {
	s, err := a.loadState()
	if err != nil {
		s = state{Token: rand.Text(), Start: startLogin}
	}
	s.Name, s.Description = a.Name, a.Description
	if port != 0 {
		s.Port = port
	}
	if s.Port == 0 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return s, err
		}
		s.Port = l.Addr().(*net.TCPAddr).Port
		l.Close()
	}
	return s, a.saveState(s)
}

func (a *App) serve() error {
	st, err := a.ensureState(0)
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", st.Port)
	log.Printf("%s on http://%s", a.Name, addr)
	return http.ListenAndServe(addr, a.handler(st.Token))
}

func (a *App) handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/{name}", a.api)
	mux.HandleFunc("GET /_paddock/events", a.events)
	mux.HandleFunc("GET /_paddock/ping", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") })
	mux.HandleFunc("GET /_paddock/icons/{name}", icon)
	mux.HandleFunc("GET /_paddock/icons.json", iconNames)
	files, _ := fs.Sub(ui, "ui")
	mux.Handle("GET /_paddock/", http.StripPrefix("/_paddock/", http.FileServerFS(files)))
	if a.web != nil {
		mux.Handle("GET /", http.FileServerFS(a.web))
	}
	return a.guard(token, mux)
}

// guard lets in only loopback hosts and holders of the token. The host check stops DNS rebinding (another site's page
// whose name now points at 127.0.0.1 still sends its own name); the token, kept in a SameSite=Strict cookie once the
// page is opened with ?token=, stops other sites' requests, and the CLI sends it as a bearer.
func (a *App) guard(token string, next http.Handler) http.Handler {
	// Cookies ignore the port, so every extension on 127.0.0.1 needs a cookie name of its own.
	cookie := "paddock-" + a.Name
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		// Any port: a page opened over an SSH forward comes in on the forward's local port.
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if t := r.URL.Query().Get("token"); t != "" && same(t, token) {
			http.SetCookie(w, &http.Cookie{Name: cookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			q := r.URL.Query()
			q.Del("token")
			u := *r.URL
			u.RawQuery = q.Encode()
			http.Redirect(w, r, u.RequestURI(), http.StatusFound)
			return
		}
		got := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			got = strings.TrimPrefix(h, "Bearer ")
		} else if c, err := r.Cookie(cookie); err == nil {
			got = c.Value
		}
		if !same(got, token) {
			http.Error(w, "unauthorized: open the address `"+a.Name+" url` prints", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func same(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func (a *App) api(w http.ResponseWriter, r *http.Request) {
	c := a.find(r.PathValue("name"))
	if c == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no command " + r.PathValue("name")})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := c.run(&Ctx{Context: r.Context(), app: a}, body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// events streams Emit's events to a page as server-sent events.
func (a *App) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no streaming", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := make(chan string, 16)
	a.mu.Lock()
	a.subs[ch] = struct{}{}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.subs, ch)
		a.mu.Unlock()
	}()
	io.WriteString(w, ": ok\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			io.WriteString(w, msg)
			flusher.Flush()
		}
	}
}

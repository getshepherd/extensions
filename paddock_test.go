package paddock

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCommandOverHTTPAndFlags(t *testing.T) {
	a := New("t", "test", nil)
	Command(a, "echo", "repeat text", func(_ *Ctx, in struct {
		Text string   `json:"text"`
		N    int      `json:"n"`
		Tags []string `json:"tag"`
	}) (string, error) {
		return strings.Repeat(in.Text, in.N) + strings.Join(in.Tags, ","), nil
	})
	srv := httptest.NewServer(a.handler("secret"))
	defer srv.Close()

	// The CLI's half: flags into the JSON body.
	fs, in, _ := a.cmds[0].flags("t")
	if err := fs.Parse([]string{"--text", "ab", "--n", "2", "--tag", "x", "--tag", "y"}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(in.Interface())

	post := func(host, auth string) (int, string) {
		req, _ := http.NewRequest("POST", srv.URL+"/api/echo", strings.NewReader(string(body)))
		if host != "" {
			req.Host = host
		}
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, strings.TrimSpace(string(b))
	}

	if code, out := post("", "secret"); code != 200 || out != `"ababx,y"` {
		t.Fatalf("with the token: %d %s", code, out)
	}
	if code, _ := post("", "wrong"); code != 401 {
		t.Fatalf("a wrong token: %d", code)
	}
	if code, _ := post("evil.example:80", "secret"); code != 403 {
		t.Fatalf("a rebound host: %d", code)
	}

	// The page's half: ?token= becomes the extension's own cookie.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(srv.URL + "/?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if c := resp.Cookies(); resp.StatusCode != 302 || len(c) != 1 || c[0].Name != "paddock-t" || c[0].Value != "secret" {
		t.Fatalf("token to cookie: %d %v", resp.StatusCode, c)
	}
}

func TestFaviconWithoutToken(t *testing.T) {
	web := fstest.MapFS{"web/favicon.svg": {Data: []byte("<svg/>")}, "web/index.html": {Data: []byte("page")}}
	a := New("t", "test", web)
	srv := httptest.NewServer(a.handler("secret"))
	defer srv.Close()
	get := func(path string) int {
		r, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if c := get("/favicon.svg"); c != 200 {
		t.Errorf("favicon: %d", c)
	}
	if c := get("/favicon.ico"); c != 404 {
		t.Errorf("no favicon.ico: %d, want the file server's 404", c)
	}
	for _, p := range []string{"/", "/index.html", "/favicon.svg/../index.html", "/_paddock/ping"} {
		if c := get(p); c != 401 {
			t.Errorf("%s: %d without the token", p, c)
		}
	}
}

func TestIcons(t *testing.T) {
	srv := httptest.NewServer(New("t", "test", nil).handler("secret"))
	defer srv.Close()
	get := func(path string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := get("/_paddock/icons/folder.svg"); code != 200 || !strings.HasPrefix(body, "<svg") {
		t.Fatalf("folder: %d %.40s", code, body)
	}
	if code, _ := get("/_paddock/icons/no-such-icon.svg"); code != 404 {
		t.Fatalf("an unknown icon: %d", code)
	}
	var names []string
	if _, body := get("/_paddock/icons.json"); json.Unmarshal([]byte(body), &names) != nil || len(names) < 1000 {
		t.Fatalf("icons.json: %d names", len(names))
	}
}

func TestPlaceCopiesTheBinaryIntoShepherdsExtensions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)                                      // macOS: ~/Library/Application Support
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config")) // elsewhere
	a := New("t", "test", nil)
	if !strings.HasPrefix(a.Dir(), home) || filepath.Base(filepath.Dir(a.Dir())) != "extensions" {
		t.Fatalf("Dir %s: not <home>/…/extensions/t", a.Dir())
	}
	for range 2 { // the second is an update, over the first
		p, err := a.place()
		if err != nil {
			t.Fatal(err)
		}
		self, _ := os.Executable()
		want, _ := os.Stat(self)
		got, err := os.Stat(p)
		if err != nil || got.Size() != want.Size() || got.Mode()&0o100 == 0 {
			t.Fatalf("%s: %v, %d bytes of %d, mode %v", p, err, got.Size(), want.Size(), got.Mode())
		}
	}
}

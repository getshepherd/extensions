package paddock

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

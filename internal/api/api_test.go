package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KalebHawkins/zero/spec"
)

func TestURLJoin(t *testing.T) {
	for _, tc := range []struct{ base, path, want string }{
		{"https://kryolabs.duckdns.org/zero", "/me", "https://kryolabs.duckdns.org/zero/api/me"},
		{"https://kryolabs.duckdns.org/zero/", "me", "https://kryolabs.duckdns.org/zero/api/me"},
		{"https://kryolabs.duckdns.org/zero-next", "/cli/device", "https://kryolabs.duckdns.org/zero-next/api/cli/device"},
		{"http://localhost:8090/a/b/", "/exercises/hello-world/runs", "http://localhost:8090/a/b/api/exercises/hello-world/runs"},
	} {
		c := New(tc.base, "", "0.1.0")
		if got := c.URL(tc.path); got != tc.want {
			t.Errorf("URL(%q) on %q = %q, want %q", tc.path, tc.base, got, tc.want)
		}
	}
}

func TestHeadersAndPrefix(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		json.NewEncoder(w).Encode(spec.MeResponse{User: spec.User{Login: "kryo", Name: "Kaleb"}})
	}))
	defer srv.Close()

	c := New(srv.URL+"/zero-next", "tok-123", "0.1.0")
	user, err := c.Me()
	if err != nil {
		t.Fatal(err)
	}
	if user.Login != "kryo" {
		t.Errorf("login = %q", user.Login)
	}
	if got.URL.Path != "/zero-next/api/me" {
		t.Errorf("path = %q, want /zero-next/api/me", got.URL.Path)
	}
	if ua := got.Header.Get("User-Agent"); ua != "zero/0.1.0" {
		t.Errorf("User-Agent = %q", ua)
	}
	if auth := got.Header.Get("Authorization"); auth != "Bearer tok-123" {
		t.Errorf("Authorization = %q", auth)
	}

	// Without a token there is no Authorization header.
	c.Token = ""
	if _, err := c.Me(); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Header["Authorization"]; ok {
		t.Error("Authorization was sent without a token")
	}
}

func TestErrorCarriesTheServerMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/exercises/nope":
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(spec.Error{Code: "not_found", Message: "There is no exercise named nope."})
		default:
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("<html>bad gateway</html>"))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "t", "0.1.0")

	_, err := c.Exercise("nope")
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *Error", err)
	}
	if apiErr.Status != 404 || apiErr.Code != "not_found" || apiErr.Message != "There is no exercise named nope." {
		t.Errorf("error = %+v", apiErr)
	}

	// An answer that is not the JSON error shape still gives a sentence.
	_, err = c.Next()
	if !errors.As(err, &apiErr) || apiErr.Status != 502 || !strings.Contains(apiErr.Message, "502") {
		t.Errorf("err = %v", err)
	}
}

func TestPollToken(t *testing.T) {
	status := http.StatusAccepted
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in spec.TokenRequest
		json.NewDecoder(r.Body).Decode(&in)
		if r.Method != http.MethodPost || r.URL.Path != "/api/cli/token" || in.DeviceCode != "dev-1" {
			t.Errorf("request = %s %s %+v", r.Method, r.URL.Path, in)
		}
		switch status {
		case http.StatusAccepted:
			w.WriteHeader(status)
			w.Write([]byte(`{"status":"pending"}`))
		case http.StatusGone:
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(spec.Error{Code: "expired", Message: "The code expired."})
		default:
			json.NewEncoder(w).Encode(spec.TokenResponse{Token: "tok", User: spec.User{Login: "kryo"}})
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "", "0.1.0")

	if _, err := c.PollToken("dev-1"); !errors.Is(err, ErrPending) {
		t.Errorf("202: err = %v, want ErrPending", err)
	}
	status = http.StatusGone
	if _, err := c.PollToken("dev-1"); !errors.Is(err, ErrExpired) {
		t.Errorf("410: err = %v, want ErrExpired", err)
	}
	status = http.StatusOK
	tok, err := c.PollToken("dev-1")
	if err != nil || tok.Token != "tok" || tok.User.Login != "kryo" {
		t.Errorf("200: %+v, %v", tok, err)
	}
}

func TestNoContentAndNullNext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/next" {
			w.Write([]byte(`{"next": null}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := New(srv.URL, "t", "0.1.0")
	if err := c.PostRun("hello-world", spec.Report{Exercise: "hello-world"}); err != nil {
		t.Errorf("PostRun: %v", err)
	}
	if err := c.PostDoctor(spec.Doctor{OK: true}); err != nil {
		t.Errorf("PostDoctor: %v", err)
	}
	next, err := c.Next()
	if err != nil || next != nil {
		t.Errorf("Next = %+v, %v; want nil", next, err)
	}
}

func TestNetError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close() // nothing listens here now
	c := New(base, "", "0.1.0")
	_, err := c.ServerConfig()
	var netErr *NetError
	if !errors.As(err, &netErr) || netErr.BaseURL != base {
		t.Errorf("err = %v, want *NetError for %s", err, base)
	}
}

func TestExerciseWithReference(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
		w.Write([]byte(`{"exercise": {"id": "life-2"}, "project": {"id": "life", "stage": 2, "stages": 2, "edit_all": ["rules.go"]},
			"reference": [{"path": "rules.go", "content": "x", "mode": "edit"}]}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "t", "0.2.0")
	if _, err := c.Exercise("life-2"); err != nil {
		t.Fatal(err)
	}
	resp, err := c.ExerciseWithReference("life-2")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/api/exercises/life-2?", "/api/exercises/life-2?reference=1"}; len(queries) != 2 || queries[0] != want[0] || queries[1] != want[1] {
		t.Errorf("requests = %q, want %q", queries, want)
	}
	if resp.Project == nil || resp.Project.ID != "life" || len(resp.Reference) != 1 {
		t.Errorf("answer = %+v", resp)
	}
}

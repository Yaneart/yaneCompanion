package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"yanecompanion/internal/config"
)

func TestRequestBoundaries(t *testing.T) {
	stop := make(chan struct{}, 1)
	h := Handler{BootID: "boot", LocalToken: strings.Repeat("a", 64), Origins: []string{"https://watch.example"}, Stop: stop}
	for _, tc := range []struct {
		name, method, path, host, origin, token string
		status                                  int
	}{
		{"hello", "GET", "/hello", config.Address, "https://watch.example", "", 200},
		{"rebind", "GET", "/hello", "evil.example:45723", "https://watch.example", "", 403},
		{"other origin", "GET", "/hello", config.Address, "https://evil.example", "", 403},
		{"null", "GET", "/hello", config.Address, "null", "", 403},
		{"missing", "GET", "/hello", config.Address, "", "", 403},
		{"no website admission", "POST", "/v1/sessions", config.Address, "https://watch.example", "", 404},
		{"no local token", "POST", "/local/exit", config.Address, "", "", 403},
		{"website cannot exit", "POST", "/local/exit", config.Address, "https://watch.example", h.LocalToken, 403},
		{"GET cannot exit", "GET", "/local/exit", config.Address, "", h.LocalToken, 404},
		{"status", "GET", "/local/status", config.Address, "", h.LocalToken, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://"+config.Address+tc.path, nil)
			r.Host = tc.host
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			r.Header.Set("X-Forwarded-Host", config.Address)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing cache policy")
			}
			if strings.Contains(w.Body.String(), h.LocalToken) || strings.Contains(w.Body.String(), "sessions.v1") {
				t.Fatal("secret or unimplemented capability exposed")
			}
			if len(stop) != 0 {
				t.Fatal("unexpected stop side effect")
			}
		})
	}
	r := httptest.NewRequest("POST", "http://"+config.Address+"/local/exit", nil)
	r.Header.Set("Authorization", "Bearer "+h.LocalToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 202 || len(stop) != 1 {
		t.Fatal("local exit failed")
	}
}

func TestOriginValidation(t *testing.T) {
	for _, origin := range []string{"null", "https://*.example", "https://user@watch.example", "http://watch.example", "https://watch.example/", "https://watch.example?x=1", "https://watch.example#x"} {
		if ValidOrigin(origin) {
			t.Fatalf("accepted %q", origin)
		}
	}
}

func TestDevelopmentOriginRequiresLocalOptIn(t *testing.T) {
	for _, origin := range []string{"http://localhost:5173", "http://127.0.0.1:5173", "http://[::1]:5173"} {
		if !ValidDevelopmentOrigin(origin) || ValidOrigin(origin) {
			t.Fatalf("development/HTTPS policy mismatch: %s", origin)
		}
		for _, configured := range []bool{false, true} {
			h := Handler{Origins: []string{origin}}
			if configured {
				h.DevelopmentOrigin = origin
			}
			r := httptest.NewRequest("GET", "http://"+config.Address+"/hello", nil)
			r.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if configured && w.Code != 200 || !configured && w.Code != 403 {
				t.Fatalf("HTTP opt-in=%v: %d", configured, w.Code)
			}
		}
	}
	for _, origin := range []string{
		"http://watch.example", "http://192.168.1.2:5173", "http://127.0.0.2:5173",
		"http://localhost.evil.example:5173", "http://127.0.0.1.evil.example:5173",
		"http://localhost@evil.example:5173", "http://evil@localhost:5173",
		"http://localhost:5173/", "http://localhost:5173?x=1", "http://localhost:5173#x",
		"http://[::ffff:127.0.0.1]:5173", "http://*.localhost:5173", "https://localhost:5173", "null", "",
	} {
		if ValidDevelopmentOrigin(origin) {
			t.Fatalf("accepted non-local or malformed development origin: %s", origin)
		}
	}
	h := Handler{DevelopmentOrigin: "http://localhost:5173"}
	for _, origin := range []string{"http://localhost:5174", "http://127.0.0.1:5173", "https://localhost:5173"} {
		r := httptest.NewRequest("GET", "http://"+config.Address+"/hello", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("non-exact development origin accepted: %s", origin)
		}
	}
}

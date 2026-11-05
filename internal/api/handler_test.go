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

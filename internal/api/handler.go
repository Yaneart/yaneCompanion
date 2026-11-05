package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"yanecompanion/internal/config"
	"yanecompanion/release"
)

type Handler struct {
	BootID, LocalToken string
	Origins            []string
	Stop               chan<- struct{}
}

func ValidOrigin(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.String() == origin && !strings.Contains(origin, "*")
}

func fail(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "retry": "none"}})
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Host != config.Address {
		fail(w, 403, "HOST_DENIED")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/local/") {
		if r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.LocalToken)) != 1 {
			fail(w, 403, "LOCAL_AUTH_REQUIRED")
			return
		}
		switch {
		case r.URL.Path == "/local/status" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(map[string]any{"product": "yaneCompanion", "helperVersion": release.Version, "bootId": h.BootID, "admission": false})
		case r.URL.Path == "/local/exit" && r.Method == "POST":
			w.WriteHeader(http.StatusAccepted)
			select {
			case h.Stop <- struct{}{}:
			default:
			}
		default:
			fail(w, 404, "NOT_FOUND")
		}
		return
	}
	w.Header().Set("Vary", "Origin")
	origin := r.Header.Get("Origin")
	allowed := false
	for _, candidate := range h.Origins {
		if origin == candidate {
			allowed = true
			break
		}
	}
	if !allowed || !ValidOrigin(origin) {
		fail(w, 403, "ORIGIN_DENIED")
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	if r.URL.Path == "/hello" && r.Method == "GET" {
		_ = json.NewEncoder(w).Encode(map[string]any{"product": "yaneCompanion", "helperVersion": release.Version, "api": map[string]int{"major": 1, "minor": 0}, "bootId": h.BootID, "capabilities": []string{}})
		return
	}
	// Pairing/controllers/sessions belong to later stages. No capability is
	// advertised until its complete contract and egress protection are verified.
	fail(w, 404, "NOT_FOUND")
}

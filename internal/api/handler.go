package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"yanecompanion/internal/config"
	"yanecompanion/internal/pairing"
	"yanecompanion/internal/sessions"
	"yanecompanion/release"
)

type Handler struct {
	BootID, LocalToken string
	Origins            []string
	DevelopmentOrigin  string
	Stop               chan<- struct{}
	Pairings           *pairing.Store
	Management         *Management
	Sessions           *sessions.Manager
}

func ValidOrigin(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.String() == origin && !strings.ContainsAny(origin, "*\\\r\n\t ")
}

// HTTP is opt-in for an exact local development origin only. Production origins
// still use ValidOrigin and never inherit this exception from the allowlist.
func ValidDevelopmentOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || !ValidOrigin("https"+strings.TrimPrefix(origin, "http")) {
		return false
	}
	return u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
}

func fail(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "retry": "none"}})
}

func respond(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}

func bearer(r *http.Request) string {
	if len(r.Header.Values("Authorization")) != 1 {
		return ""
	}
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(value, "Bearer ")
}

func jsonBody(w http.ResponseWriter, r *http.Request, value any) bool {
	_, ok := readJSON(w, r, value, 1024)
	return ok
}

func readJSON(w http.ResponseWriter, r *http.Request, value any, limit int64) ([]byte, bool) {
	typeName, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typeName != "application/json" || len(r.Header.Values("Content-Type")) != 1 {
		fail(w, 415, "JSON_REQUIRED")
		return nil, false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		fail(w, 413, "INPUT_TOO_LARGE")
		return nil, false
	}
	if err != nil || !uniqueObject(data) {
		fail(w, 400, "INVALID_REQUEST")
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, 400, "INVALID_REQUEST")
		return nil, false
	}
	return data, true
}

// Reject duplicate keys and non-object JSON before decoding consent decisions.
func uniqueObject(data []byte) bool {
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func(int, bool) bool
	walk = func(depth int, root bool) bool {
		if depth > 32 {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return !root
		}
		if root && delim != '{' {
			return false
		}
		if delim != '{' && delim != '[' {
			return false
		}
		seen := map[string]bool{}
		for d.More() {
			if delim == '{' {
				key, err := d.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				name = strings.ToLower(name)
				if !ok || seen[name] {
					return false
				}
				seen[name] = true
			}
			if !walk(depth+1, false) {
				return false
			}
		}
		end, err := d.Token()
		return err == nil && (delim == '{' && end == json.Delim('}') || delim == '[' && end == json.Delim(']'))
	}
	return walk(0, true) && d.Decode(new(any)) == io.EOF
}

func pairError(w http.ResponseWriter, err error) {
	status := 400
	switch {
	case errors.Is(err, pairing.ErrRequired), errors.Is(err, pairing.ErrRequest):
		status = 401
	case errors.Is(err, pairing.ErrBusy):
		status = 409
	case errors.Is(err, pairing.ErrRate):
		status = 429
	case errors.Is(err, pairing.ErrExpired):
		status = 410
	case errors.Is(err, pairing.ErrStorage):
		status = 503
	}
	fail(w, status, err.Error())
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Host != config.Address || r.URL.Host != "" && r.URL.Host != config.Address {
		fail(w, 403, "HOST_DENIED")
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.RawPath != "" || strings.Contains(r.URL.Path, "//") {
		fail(w, 400, "INVALID_REQUEST")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/local/") {
		h.local(w, r)
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
	allowed = allowed && ValidOrigin(origin) || origin == h.DevelopmentOrigin && ValidDevelopmentOrigin(origin)
	if !allowed || len(r.Header.Values("Origin")) != 1 {
		fail(w, 403, "ORIGIN_DENIED")
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	methods := routeMethods(r.URL.Path)
	if r.Method == "OPTIONS" {
		preflight(w, r, methods)
		return
	}
	if !strings.Contains(","+methods+",", ","+r.Method+",") {
		fail(w, 404, "NOT_FOUND")
		return
	}
	if r.URL.Path == "/hello" {
		capabilities := []string{}
		if h.Pairings != nil {
			capabilities = append(capabilities, "pairing.v1")
		}
		if h.Sessions != nil {
			capabilities = append(capabilities, "torrent-admission.v1", "torrent-files.v1")
		}
		respond(w, 200, map[string]any{"product": "yaneCompanion", "helperVersion": release.Version, "api": map[string]int{"major": 1, "minor": 0}, "bootId": h.BootID, "capabilities": capabilities})
		return
	}
	if h.Pairings == nil {
		fail(w, 404, "NOT_FOUND")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/controllers") || strings.HasPrefix(r.URL.Path, "/v1/sessions") || strings.HasPrefix(r.URL.Path, "/v1/operations") {
		if h.Sessions == nil {
			fail(w, 404, "NOT_FOUND")
			return
		}
		h.sessionAPI(w, r, origin)
		return
	}
	if r.URL.Path == "/v1/pairings" {
		var body struct{}
		if !jsonBody(w, r, &body) {
			return
		}
		created, err := h.Pairings.Create(origin)
		if err != nil {
			pairError(w, err)
			return
		}
		created.Origin = ""
		respond(w, 201, created)
		return
	}
	if r.URL.Path == "/v1/pairings/current" {
		grant, err := h.Pairings.Authenticate(origin, bearer(r))
		if err != nil {
			pairError(w, err)
			return
		}
		if r.Method == "DELETE" {
			if err := h.Pairings.Revoke(grant.ID); err != nil {
				pairError(w, err)
				return
			}
			respond(w, 204, nil)
		} else {
			respond(w, 200, map[string]string{"state": "paired"})
		}
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/pairings/")
	if r.Method == "DELETE" {
		if err := h.Pairings.Cancel(origin, id, bearer(r)); err != nil {
			pairError(w, err)
			return
		}
		respond(w, 200, pairing.Result{State: "cancelled"})
		return
	}
	result, err := h.Pairings.Poll(origin, id, bearer(r))
	if err != nil {
		pairError(w, err)
		return
	}
	respond(w, 200, result)
}

func routeMethods(path string) string {
	switch path {
	case "/hello":
		return "GET"
	case "/v1/pairings":
		return "POST"
	case "/v1/pairings/current":
		return "GET,DELETE"
	}
	if methods := sessionMethods(path); methods != "" {
		return methods
	}
	if strings.HasPrefix(path, "/v1/pairings/") && len(strings.TrimPrefix(path, "/v1/pairings/")) == 32 {
		return "GET,DELETE"
	}
	return ""
}

func preflight(w http.ResponseWriter, r *http.Request, methods string) {
	w.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
	method := r.Header.Get("Access-Control-Request-Method")
	if method == "" || !strings.Contains(","+methods+",", ","+method+",") {
		fail(w, 403, "PREFLIGHT_DENIED")
		return
	}
	for _, line := range r.Header.Values("Access-Control-Request-Headers") {
		for _, header := range strings.Split(line, ",") {
			if header = strings.ToLower(strings.TrimSpace(header)); header != "authorization" && header != "content-type" && header != "idempotency-key" && header != "if-match" {
				fail(w, 403, "PREFLIGHT_DENIED")
				return
			}
		}
	}
	w.Header().Set("Access-Control-Allow-Methods", strings.ReplaceAll(methods, ",", ", "))
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, If-Match")
	w.WriteHeader(204)
}

func (h Handler) local(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/local/manage" && r.Method == "GET" {
		managementPage(w)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/local/manage/") && r.URL.Path != "/local/manage/open" {
		h.manage(w, r)
		return
	}
	if r.Header.Get("Origin") != "" || len(r.Header.Values("Origin")) != 0 || h.LocalToken == "" || subtle.ConstantTimeCompare([]byte(bearer(r)), []byte(h.LocalToken)) != 1 {
		fail(w, 403, "LOCAL_AUTH_REQUIRED")
		return
	}
	switch {
	case r.URL.Path == "/local/status" && r.Method == "GET":
		respond(w, 200, map[string]any{"product": "yaneCompanion", "helperVersion": release.Version, "bootId": h.BootID, "admission": h.Sessions != nil, "playback": false})
	case r.URL.Path == "/local/exit" && r.Method == "POST":
		w.WriteHeader(202)
		select {
		case h.Stop <- struct{}{}:
		default:
		}
	case r.URL.Path == "/local/manage/open" && r.Method == "POST" && h.Management != nil:
		token, err := h.Management.open()
		if err != nil {
			fail(w, 503, "MANAGEMENT_UNAVAILABLE")
			return
		}
		respond(w, 200, map[string]string{"managementToken": token})
	default:
		fail(w, 404, "NOT_FOUND")
	}
}

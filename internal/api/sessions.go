package api

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"yanecompanion/internal/locator"
	"yanecompanion/internal/pairing"
	"yanecompanion/internal/sessions"
)

var resourceID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func sessionMethods(path string) string {
	switch path {
	case "/v1/controllers", "/v1/controllers/current/heartbeat", "/v1/sessions":
		return "POST"
	case "/v1/controllers/current":
		return "DELETE"
	case "/v1/sessions/active":
		return "GET"
	}
	parts := strings.Split(strings.TrimPrefix(path, "/v1/"), "/")
	if len(parts) == 2 && parts[0] == "sessions" && resourceID.MatchString(parts[1]) {
		return "GET,DELETE"
	}
	if len(parts) == 3 && parts[0] == "sessions" && resourceID.MatchString(parts[1]) && parts[2] == "commands" {
		return "POST"
	}
	if len(parts) == 2 && parts[0] == "operations" && len(parts[1]) == 36 {
		return "GET,DELETE"
	}
	return ""
}
func (h Handler) sessionAPI(w http.ResponseWriter, r *http.Request, origin string) {
	token := bearer(r)
	failure := func(err error) bool {
		if err == nil {
			return false
		}
		status, code := sessions.Code(err)
		fail(w, status, code)
		return true
	}
	if r.URL.Path == "/v1/controllers" {
		if !jsonBody(w, r, &struct{}{}) {
			return
		}
		var result sessions.Controller
		err := h.Pairings.UseGrant(origin, token, func(grant pairing.Grant) error {
			var err error
			result, err = h.Sessions.CreateController(origin, grant.ID)
			return err
		})
		if err == pairing.ErrRequired {
			pairError(w, err)
		} else if !failure(err) {
			respond(w, 201, result)
		}
		return
	}
	if failure(h.Sessions.Authorize(origin, token)) {
		return
	}
	switch r.URL.Path {
	case "/v1/controllers/current":
		if !failure(h.Sessions.DeleteController(origin, token)) {
			respond(w, 204, nil)
		}
		return
	case "/v1/controllers/current/heartbeat":
		if !jsonBody(w, r, &struct{}{}) {
			return
		}
		expires, err := h.Sessions.Heartbeat(origin, token)
		if !failure(err) {
			respond(w, 200, map[string]any{"expiresAt": expires})
		}
		return
	case "/v1/sessions":
		var body struct {
			Locator locator.Locator `json:"locator"`
		}
		data, ok := readJSON(w, r, &body, 3<<20)
		if !ok {
			return
		}
		if len(r.Header.Values("Idempotency-Key")) != 1 {
			fail(w, 400, "INVALID_INPUT")
			return
		}
		result, err := h.Sessions.Create(origin, token, r.Header.Get("Idempotency-Key"), data, body.Locator)
		if !failure(err) {
			respond(w, 202, result)
		}
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/"), "/")
	id := parts[1]
	if parts[0] == "operations" {
		if r.Method == "DELETE" {
			revision, err := strconv.Atoi(strings.Trim(r.Header.Get("If-Match"), `"`))
			if err != nil || revision < 1 || len(r.Header.Values("If-Match")) != 1 {
				fail(w, 400, "INVALID_INPUT")
				return
			}
			if !failure(h.Sessions.Cancel(origin, token, id, revision)) {
				respond(w, 204, nil)
			}
		} else {
			result, err := h.Sessions.Operation(origin, token, id)
			if !failure(err) {
				respond(w, 200, result)
			}
		}
		return
	}
	if len(parts) == 3 {
		var body struct {
			Action   string `json:"action"`
			Revision int    `json:"expectedRevision"`
			FileRef  string `json:"fileRef"`
		}
		data, ok := readJSON(w, r, &body, 1024)
		if !ok {
			return
		}
		if body.Action != "select" {
			fail(w, 422, "UNSUPPORTED_CAPABILITY")
			return
		}
		if len(r.Header.Values("Idempotency-Key")) != 1 {
			fail(w, 400, "INVALID_INPUT")
			return
		}
		result, err := h.Sessions.Select(origin, token, id, r.Header.Get("Idempotency-Key"), data, body.Revision, body.FileRef)
		if !failure(err) {
			respond(w, 202, result)
		}
		return
	}
	if r.Method == "DELETE" {
		if !failure(h.Sessions.Stop(origin, token, id)) {
			respond(w, 204, nil)
		}
		return
	}
	result, err := h.Sessions.Snapshot(origin, token, id)
	if !failure(err) {
		respond(w, 200, result)
	}
}

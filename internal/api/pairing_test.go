package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yanecompanion/internal/config"
	"yanecompanion/internal/pairing"
)

const website = "https://watch.example"
const selfOrigin = "http://" + config.Address

func pairingHandler(t *testing.T) Handler {
	t.Helper()
	store, err := pairing.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Handler{BootID: "boot", LocalToken: strings.Repeat("a", 64), Origins: []string{website, "https://other.example"}, Pairings: store, Management: &Management{}}
}

func call(h Handler, method, path, origin, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, selfOrigin+path, strings.NewReader(body))
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func create(t *testing.T, h Handler) pairing.Request {
	t.Helper()
	w := call(h, "POST", "/v1/pairings", website, "", "{}")
	var created pairing.Request
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &created) != nil || len(created.PollToken) != 64 || len(created.Code) != 6 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	return created
}

func managementToken(t *testing.T, h Handler) string {
	t.Helper()
	w := call(h, "POST", "/local/manage/open", "", h.LocalToken, "")
	var result struct {
		Token string `json:"managementToken"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Token) != 64 {
		t.Fatal("local management launch failed")
	}
	return result.Token
}

func TestConsentAPIAndLocalConfirmation(t *testing.T) {
	h := pairingHandler(t)
	r := create(t, h)
	path := "/v1/pairings/" + r.RequestID
	if w := call(h, "GET", path, website, r.PollToken, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"pending"`) {
		t.Fatal("request did not wait for local consent")
	}
	for _, tc := range []struct{ origin, token string }{{website, ""}, {website, h.LocalToken}, {"https://other.example", r.PollToken}} {
		if w := call(h, "GET", path, tc.origin, tc.token, ""); w.Code != 401 {
			t.Fatal("foreign/missing poll proof accepted")
		}
	}
	management := managementToken(t, h)
	decision := `{"requestId":"` + r.RequestID + `","code":"` + r.Code + `","approve":true}`
	duplicate := `{"requestId":"` + r.RequestID + `","code":"` + r.Code + `","approve":false,"Approve":true}`
	if w := call(h, "POST", "/local/manage/decision", selfOrigin, management, duplicate); w.Code != 400 {
		t.Fatal("case-variant duplicate consent fields accepted")
	}
	for _, tc := range []struct{ origin, token string }{{website, management}, {"", management}, {selfOrigin, r.PollToken}, {selfOrigin, h.LocalToken}} {
		if w := call(h, "POST", "/local/manage/decision", tc.origin, tc.token, decision); w.Code != 403 {
			t.Fatal("website or wrong credential approved request")
		}
	}
	if w := call(h, "POST", "/local/manage/decision", selfOrigin, management, decision); w.Code != 204 {
		t.Fatalf("local decision: %d %s", w.Code, w.Body.String())
	}
	w := call(h, "GET", path, website, r.PollToken, "")
	var result pairing.Result
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.State != "approved" || result.PairingToken == "" {
		t.Fatal("approved token missing")
	}
	if again := call(h, "GET", path, website, r.PollToken, ""); again.Body.String() != w.Body.String() {
		t.Fatal("approved poll not repeatable")
	}
	if w := call(h, "GET", "/v1/pairings/current", website, result.PairingToken, ""); w.Code != 200 {
		t.Fatal("approved grant not authenticated")
	}
	if w := call(h, "GET", "/v1/pairings/current", "https://other.example", result.PairingToken, ""); w.Code != 401 {
		t.Fatal("pairing leaked across origins")
	}
	if w := call(h, "DELETE", path, website, r.PollToken, ""); w.Code != 200 {
		t.Fatal("late cancellation failed")
	}
	if w := call(h, "GET", "/v1/pairings/current", website, result.PairingToken, ""); w.Code != 401 {
		t.Fatal("cancel left usable pairing")
	}
}

func TestNarrowPreflightAndJSON(t *testing.T) {
	h := pairingHandler(t)
	for _, tc := range []struct {
		method, headers string
		status          int
	}{{"POST", "content-type", 204}, {"DELETE", "authorization", 403}, {"POST", "content-type, authorization", 204}, {"POST", "x-arbitrary", 403}, {"", "content-type", 403}} {
		r := httptest.NewRequest("OPTIONS", selfOrigin+"/v1/pairings", nil)
		r.Header.Set("Origin", website)
		r.Header.Set("Access-Control-Request-Method", tc.method)
		r.Header.Set("Access-Control-Request-Headers", tc.headers)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Access-Control-Allow-Credentials") != "" || w.Header().Get("Access-Control-Allow-Origin") != website {
			t.Fatal("preflight contract failed")
		}
	}
	for _, body := range []string{"null", "[]", `{"locator":"magnet:invalid"}`, "{}{}", "{", strings.Repeat("x", 1025), `{"approve":true,"approve":false}`} {
		want := 400
		if len(body) > 1024 {
			want = 413
		}
		if w := call(h, "POST", "/v1/pairings", website, "", body); w.Code != want {
			t.Fatalf("malformed JSON accepted: %q %d", body, w.Code)
		}
	}
	r := httptest.NewRequest("POST", selfOrigin+"/v1/pairings", strings.NewReader("{}"))
	r.Header.Set("Origin", website)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal("simple non-JSON request created consent")
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Add("Origin", website)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("duplicate origin accepted")
	}
	requests, _ := h.Pairings.Snapshot()
	if len(requests) != 0 {
		t.Fatal("invalid requests had side effects")
	}
}

func TestManagementProofRotationAndPageIsolation(t *testing.T) {
	h := pairingHandler(t)
	first := managementToken(t, h)
	second := managementToken(t, h)
	if first == second {
		t.Fatal("management proof reused")
	}
	if w := call(h, "POST", "/local/manage/snapshot", selfOrigin, first, "{}"); w.Code != 403 {
		t.Fatal("old management proof remained active")
	}
	if w := call(h, "POST", "/local/manage/snapshot", selfOrigin, second, "{}"); w.Code != 200 {
		t.Fatal("management snapshot unavailable")
	}
	if w := call(h, "POST", "/local/manage/open", website, h.LocalToken, ""); w.Code != 403 {
		t.Fatal("website opened management authority")
	}
	w := call(h, "GET", "/local/manage", "", "", "")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("management page boundary missing")
	}
	if strings.Contains(w.Body.String(), first) || strings.Contains(w.Body.String(), second) || strings.Contains(w.Body.String(), h.LocalToken) {
		t.Fatal("management page exposed a bearer")
	}
}

func TestRevocationAPIAndUnsupportedPlayback(t *testing.T) {
	h := pairingHandler(t)
	r := create(t, h)
	if err := h.Pairings.Decide(r.RequestID, r.Code, true); err != nil {
		t.Fatal(err)
	}
	result, _ := h.Pairings.Poll(website, r.RequestID, r.PollToken)
	if w := call(h, "DELETE", "/v1/pairings/current", website, result.PairingToken, ""); w.Code != 204 {
		t.Fatal("pairing revoke failed")
	}
	if w := call(h, "GET", "/v1/pairings/current", website, result.PairingToken, ""); w.Code != 401 {
		t.Fatal("revoked bearer accepted")
	}
	w := call(h, "GET", "/hello", website, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "pairing.v1") || strings.Contains(w.Body.String(), "sessions.v1") {
		t.Fatal("false capability advertised")
	}
	for _, path := range []string{"/v1/controllers", "/v1/sessions"} {
		if w := call(h, http.MethodPost, path, website, result.PairingToken, "{}"); w.Code != 404 {
			t.Fatal("unimplemented admission exposed")
		}
	}
}

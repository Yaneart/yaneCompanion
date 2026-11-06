package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yanecompanion/internal/config"
	"yanecompanion/internal/engine"
	"yanecompanion/internal/locator"
	"yanecompanion/internal/sessions"
)

type admissionEngine struct{ adds atomic.Int32 }

func (e *admissionEngine) Admit(context.Context, locator.Input) error { e.adds.Add(1); return nil }
func (e *admissionEngine) Files(context.Context, string) ([]engine.File, error) {
	return []engine.File{{Index: 1, Path: "fixture.mkv", Length: 16384}}, nil
}
func (e *admissionEngine) Download(context.Context, string, engine.File) (int64, error) {
	return 16384, nil
}
func (e *admissionEngine) Drop(context.Context, string) error { return nil }
func TestSessionBoundaryAndRevocation(t *testing.T) {
	h := pairingHandler(t)
	engine := &admissionEngine{}
	h.Sessions = sessions.New(engine)
	defer h.Sessions.Close()
	h.Pairings.OnRevoke = h.Sessions.Revoke
	request := create(t, h)
	if err := h.Pairings.Decide(request.RequestID, request.Code, true); err != nil {
		t.Fatal(err)
	}
	result, err := h.Pairings.Poll(website, request.RequestID, request.PollToken)
	if err != nil {
		t.Fatal(err)
	}
	w := call(h, "POST", "/v1/controllers", website, result.PairingToken, "{}")
	var controller sessions.Controller
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &controller) != nil {
		t.Fatal("controller creation failed", w.Body.String())
	}
	if w := call(h, "GET", "/v1/sessions/active", website, controller.Token, ""); w.Code != 404 || engine.adds.Load() != 0 {
		t.Fatal("GET admitted torrent")
	}
	key := "123e4567-e89b-42d3-a456-426614174000"
	body := `{"locator":{"kind":"magnet","value":"magnet:?xt=urn:btih:` + strings.Repeat("a", 40) + `"}}`
	post := func(origin, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+config.Address+"/v1/sessions", strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, token := range []string{"", result.PairingToken, h.LocalToken, request.PollToken} {
		if w := post(website, token, body); w.Code != 401 {
			t.Fatal("wrong bearer admitted work", w.Code)
		}
	}
	if w := post("https://other.example", controller.Token, body); w.Code != 401 {
		t.Fatal("controller crossed origin")
	}
	for _, invalid := range []string{`{"locator":{"kind":"magnet","Kind":"metainfo"}}`, `{"locator":{"kind":"magnet","value":"x","torrentUrl":"http://127.0.0.1"}}`, `{"locator":null}`, `{"locator":{"kind":"magnet","value":"http://127.0.0.1"}}`} {
		if w := post(website, controller.Token, invalid); w.Code != 400 {
			t.Fatal("invalid locator accepted", w.Code)
		}
	}
	if w := post(website, controller.Token, strings.Repeat(" ", 3<<20)+body); w.Code != 413 {
		t.Fatal("oversize envelope accepted", w.Code)
	}
	w = post(website, controller.Token, body)
	if w.Code != 202 {
		t.Fatal("validated admission failed", w.Body.String())
	}
	replay := post(website, controller.Token, body)
	if replay.Body.String() != w.Body.String() {
		t.Fatal("admission not replayed")
	}
	deadline := time.Now().Add(time.Second)
	for engine.adds.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if w := call(h, "DELETE", "/v1/pairings/current", website, result.PairingToken, ""); w.Code != 204 {
		t.Fatal("revoke failed")
	}
	if w := call(h, "GET", "/v1/sessions/active", website, controller.Token, ""); w.Code != 401 {
		t.Fatal("revoked controller survived")
	}
	if engine.adds.Load() != 1 {
		t.Fatal("unexpected extra engine admission")
	}
}

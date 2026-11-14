package api

import (
	"testing"
)

func TestLC011MaintenanceHasNoBrowserOrManagementRoute(t *testing.T) {
	h := pairingHandler(t)
	for _, path := range []string{"/v1/update", "/v1/diagnostics", "/v1/clean-data", "/local/update", "/local/clean-data", "/local/manage/update", "/local/manage/clean-data"} {
		for _, origin := range []string{website, selfOrigin, ""} {
			w := call(h, "POST", path, origin, h.LocalToken, "{}")
			if w.Code < 400 {
				t.Fatalf("maintenance route exposed: %s", path)
			}
		}
	}
}

func TestDesktopShowRequiresLocalProofAndNoBrowserOrigin(t *testing.T) {
	h := pairingHandler(t)
	shows := 0
	h.Show = func() { shows++ }
	for _, request := range []struct{ method, origin, token string }{{"POST", website, h.LocalToken}, {"POST", selfOrigin, h.LocalToken}, {"POST", "", ""}, {"GET", "", h.LocalToken}} {
		w := call(h, request.method, "/local/show", request.origin, request.token, "")
		if w.Code < 400 || shows != 0 {
			t.Fatal("untrusted caller opened desktop")
		}
	}
	if w := call(h, "POST", "/local/show", "", h.LocalToken, ""); w.Code != 202 || shows != 1 {
		t.Fatal("local show failed")
	}
}

func TestDesktopOriginChangesApplyToEveryRequest(t *testing.T) {
	h := pairingHandler(t)
	h.Origins = nil
	allowed := website
	h.OriginAllowed = func(origin string) bool { return origin == allowed }
	if w := call(h, "GET", "/hello", website, "", ""); w.Code != 200 {
		t.Fatal("configured origin refused")
	}
	allowed = "https://new.example"
	if w := call(h, "GET", "/hello", website, "", ""); w.Code != 403 {
		t.Fatal("old origin still allowed")
	}
	if w := call(h, "GET", "/hello", allowed, "", ""); w.Code != 200 {
		t.Fatal("new origin refused")
	}
	allowed = "http://unsafe.example"
	if w := call(h, "GET", "/hello", allowed, "", ""); w.Code != 403 {
		t.Fatal("dynamic callback bypassed HTTPS policy")
	}
}

package api

import (
	"testing"
)

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

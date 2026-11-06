package engine

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrivateMediaRangeRequiresExactAuthenticatedResponse(t *testing.T) {
	for _, variant := range []string{"valid", "status", "size", "range", "redirect"} {
		t.Run(variant, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				user, pass, ok := r.BasicAuth()
				if !ok || user != "companion" || pass != "private" || r.Header.Get("Origin") != "" || r.Header.Get("Range") != "bytes=4-7" || r.URL.Query().Get("index") != "2" {
					t.Error("private worker request lost its boundary")
				}
				if variant == "redirect" {
					w.Header().Set("Location", "/other")
					w.WriteHeader(302)
					return
				}
				w.Header().Set("Content-Range", "bytes 4-7/16")
				if variant == "range" {
					w.Header().Set("Content-Range", "bytes 0-3/16")
				}
				w.Header().Set("Content-Length", "4")
				if variant == "size" {
					w.Header().Set("Content-Length", "5")
				}
				status := 206
				if variant == "status" {
					status = 200
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte("test"))
			}))
			defer server.Close()
			e := &Engine{address: server.URL, password: "private", client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
			file := File{Index: 2, Length: 16}
			body, err := e.OpenRange(context.Background(), "hash", file, 4, 7)
			if variant != "valid" {
				if err == nil {
					body.Close()
					t.Fatal("invalid media response accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(body)
			body.Close()
			if err != nil || string(data) != "test" {
				t.Fatal(err)
			}
			for _, span := range [][2]int64{{-1, 1}, {5, 4}, {0, 16}} {
				if b, err := e.OpenRange(context.Background(), "hash", file, span[0], span[1]); err == nil {
					b.Close()
					t.Fatal("unsafe range accepted")
				}
			}
			if calls != 1 {
				t.Fatal("invalid range reached the engine")
			}
		})
	}
}

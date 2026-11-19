package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExpiredMetadataIsNotAnEngineFailure(t *testing.T) {
	for _, status := range []int{404, 500, 401, 200} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"private engine detail"}`))
			}))
			defer server.Close()
			e := &Engine{address: server.URL, password: "private", client: server.Client()}
			_, err := e.Files(context.Background(), "hash")
			want := ErrUnavailable
			if status == 404 {
				want = ErrNoPeers
			}
			if !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
		})
	}
}

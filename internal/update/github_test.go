package update

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type redirectTransport func(*http.Request) (*http.Response, error)

func (send redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) { return send(req) }

func TestGitHubAssetRedirect(t *testing.T) {
	const asset = "https://github.com/Yaneart/yaneCompanion/releases/download/v0.3.0/release.json"
	const storage = "https://release-assets.githubusercontent.com/github-production-release-asset/123/a1b2-c3d4"
	const query = "?sp=r&spr=https&sig=abc%3D&jwt=a.b.c&response-content-type=application%2Foctet-stream"
	for _, tc := range []struct {
		name, origin, location string
		allow                  bool
	}{
		{"asset", asset, storage + query, true},
		{"other origin", strings.Replace(asset, "github.com", "example.com", 1), storage + query, false},
		{"latest alias", strings.Replace(asset, "download/v0.3.0", "latest/download", 1), storage + query, false},
		{"http", asset, strings.Replace(storage, "https://", "http://", 1) + query, false},
		{"other host", asset, strings.Replace(storage, ".com/", ".com.example/", 1) + query, false},
		{"port", asset, strings.Replace(storage, ".com/", ".com:443/", 1) + query, false},
		{"credentials", asset, strings.Replace(storage, "://", "://user@", 1) + query, false},
		{"storage path", asset, strings.Replace(storage, "release-asset/", "repository/", 1) + query, false},
		{"missing signature", asset, storage + "?sp=r&spr=https", false},
		{"write permission", asset, storage + strings.Replace(query, "sp=r", "sp=w", 1), false},
		{"duplicate parameter", asset, storage + query + "&sig=other", false},
		{"unknown parameter", asset, storage + query + "&unexpected=value", false},
		{"fragment", asset, storage + query + "#fragment", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := New("linux-deb-amd64", "0.3.0", "", "")
			client.HTTP.Transport = redirectTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				response := &http.Response{StatusCode: http.StatusFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}
				response.Header.Set("Location", tc.location)
				if calls > 1 && req.Header.Get("Referer") != "" {
					t.Error("forwarded Referer to asset storage")
				}
				return response, nil
			})
			// Every response redirects, including storage. Only the first hop may be followed.
			if _, err := client.get(t.Context(), tc.origin, MaxManifest); err == nil || err.Error() != "UPDATE_DOWNLOAD_INVALID" {
				t.Fatalf("unexpected redirect result: %v", err)
			}
			if (tc.allow && calls != 2) || (!tc.allow && calls != 1) {
				t.Fatalf("unexpected request count: %d", calls)
			}
		})
	}
}

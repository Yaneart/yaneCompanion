package update

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	githubDownloadPath = regexp.MustCompile(`^/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/releases/download/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	githubStoragePath  = regexp.MustCompile(`^/github-production-release-asset/[0-9]+/[0-9a-f-]+$`)
)

// GitHub sends versioned release asset requests directly to signed asset storage.
func githubAssetRedirect(req *http.Request, via []*http.Request) error {
	if len(via) != 1 || via[0].URL.Host != "github.com" || !HTTPS(via[0].URL.String()) ||
		!githubDownloadPath.MatchString(via[0].URL.EscapedPath()) {
		return http.ErrUseLastResponse
	}
	u := req.URL
	if u.Scheme != "https" || u.Host != "release-assets.githubusercontent.com" || u.User != nil ||
		u.Fragment != "" || !githubStoragePath.MatchString(u.EscapedPath()) || len(u.RawQuery) > 8192 {
		return http.ErrUseLastResponse
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || query.Get("sp") != "r" || query.Get("spr") != "https" || query.Get("sig") == "" {
		return http.ErrUseLastResponse
	}
	for name, values := range query {
		if len(values) != 1 || values[0] == "" || strings.ContainsAny(values[0], "\r\n\x00") {
			return http.ErrUseLastResponse
		}
		switch name {
		case "sp", "sv", "sr", "spr", "se", "rscd", "rsct", "skoid", "sktid", "skt", "ske", "sks", "skv", "sig", "jwt",
			"response-content-disposition", "response-content-type":
		default:
			return http.ErrUseLastResponse
		}
	}
	req.Header.Del("Referer")
	return nil
}

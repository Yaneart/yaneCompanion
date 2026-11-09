package config

import (
	"net/url"
	"strings"
)

func ValidOrigin(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.String() == origin && !strings.ContainsAny(origin, "*\\\r\n\t ")
}

func ValidDevelopmentOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || !ValidOrigin("https"+strings.TrimPrefix(origin, "http")) {
		return false
	}
	return u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
}

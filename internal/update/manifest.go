// Package update verifies owner-signed full packages. It never executes them.
package update

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Injected by the owner at build time, never supplied by the website or feed.
var FeedURL, PublicKey string

const MaxManifest = 64 << 10
const MaxPackage = 512 << 20
const SettingsSchema = 1

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type API struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

type Package struct {
	Platform string `json:"platform"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

type Release struct {
	Version        string    `json:"version"`
	API            API       `json:"api"`
	SettingsSchema int       `json:"settingsSchema"`
	Packages       []Package `json:"packages"`
}

type Manifest struct {
	Schema      int       `json:"schema"`
	Sequence    uint64    `json:"sequence"`
	PublishedAt time.Time `json:"publishedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Current     Release   `json:"current"`
	Previous    *Release  `json:"previous,omitempty"`
}

// Payload is base64 of canonical UTF-8 JSON. The exact bytes, with a domain
// separator, are signed; there is no ambiguous JSON reserialization to verify.
type Envelope struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

const domain = "yaneCompanion release manifest v1\n"

func strictJSON(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("UPDATE_MANIFEST_INVALID")
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(bytes.TrimSpace(data), canonical) {
		return errors.New("UPDATE_MANIFEST_INVALID")
	}
	return nil
}

func HTTPS(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.ContainsAny(value, "\r\n\t ")
}

func Filename(platform, version string) string {
	switch platform {
	case "windows-amd64":
		return "yanecompanion-" + version + "-windows-amd64-setup.exe"
	case "linux-deb-amd64":
		return "yanecompanion_" + version + "_amd64.deb"
	case "linux-arch-amd64":
		return "yanecompanion-bin-" + version + "-1-x86_64.pkg.tar.zst"
	case "linux-archive-amd64":
		return "yanecompanion-" + version + "-linux-amd64.tar.gz"
	}
	return ""
}

// Compare accepts stable releases and the existing development suffix only.
func Compare(a, b string) (int, error) {
	parse := func(s string) ([3]uint64, error) {
		var out [3]uint64
		s = strings.TrimSuffix(s, "-dev")
		if len(s) > 32 || !versionPattern.MatchString(s) {
			return out, errors.New("UPDATE_VERSION_INVALID")
		}
		for i, part := range strings.Split(s, ".") {
			v, err := strconv.ParseUint(part, 10, 32)
			if err != nil {
				return out, errors.New("UPDATE_VERSION_INVALID")
			}
			out[i] = v
		}
		return out, nil
	}
	x, err := parse(a)
	if err != nil {
		return 0, err
	}
	y, err := parse(b)
	if err != nil {
		return 0, err
	}
	for i := range x {
		if x[i] < y[i] {
			return -1, nil
		}
		if x[i] > y[i] {
			return 1, nil
		}
	}
	return 0, nil
}

func (m Manifest) Validate(now time.Time) error {
	if m.Schema != 1 || m.Sequence == 0 || m.PublishedAt.IsZero() || m.PublishedAt.After(now.Add(5*time.Minute)) || !m.ExpiresAt.After(now) || !m.ExpiresAt.After(m.PublishedAt) || m.ExpiresAt.Sub(m.PublishedAt) > 30*24*time.Hour {
		return errors.New("UPDATE_MANIFEST_EXPIRED")
	}
	releases := []Release{m.Current}
	if m.Previous != nil {
		order, err := Compare(m.Previous.Version, m.Current.Version)
		if err != nil || order >= 0 {
			return errors.New("UPDATE_MANIFEST_INVALID")
		}
		releases = append(releases, *m.Previous)
	}
	for _, r := range releases {
		if _, err := Compare(r.Version, r.Version); err != nil {
			return errors.New("UPDATE_MANIFEST_INVALID")
		}
		if !versionPattern.MatchString(r.Version) || len(r.Version) > 32 || r.API.Major < 1 || r.API.Minor < 0 || r.SettingsSchema < 1 || len(r.Packages) == 0 || len(r.Packages) > 4 {
			return errors.New("UPDATE_MANIFEST_INVALID")
		}
		seen := map[string]bool{}
		for _, p := range r.Packages {
			name := Filename(p.Platform, r.Version)
			u, _ := url.Parse(p.URL)
			if name == "" || p.Name != name || !HTTPS(p.URL) || u.Path == "" || !strings.HasSuffix(u.Path, "/"+name) || u.RawPath != "" || seen[p.Platform] || p.Size <= 0 || p.Size > MaxPackage || !hashPattern.MatchString(p.SHA256) {
				return errors.New("UPDATE_MANIFEST_INVALID")
			}
			seen[p.Platform] = true
		}
	}
	return nil
}

func Verify(data []byte, key string, now time.Time) (Manifest, error) {
	m, err := VerifyHistorical(data, key)
	if err != nil {
		return Manifest{}, err
	}
	return m, m.Validate(now)
}

// VerifyHistorical is for the offline owner tool to reuse a previously signed
// release as rollback metadata. Network update checks always use Verify.
func VerifyHistorical(data []byte, key string) (Manifest, error) {
	var m Manifest
	public, err := hex.DecodeString(key)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return m, errors.New("UPDATE_NOT_CONFIGURED")
	}
	var envelope Envelope
	if len(data) > MaxManifest || strictJSON(data, &envelope) != nil {
		return m, errors.New("UPDATE_MANIFEST_INVALID")
	}
	payload, err := base64.StdEncoding.Strict().DecodeString(envelope.Payload)
	signature, sigErr := base64.StdEncoding.Strict().DecodeString(envelope.Signature)
	if err != nil || sigErr != nil || !ed25519.Verify(public, append([]byte(domain), payload...), signature) {
		return m, errors.New("UPDATE_SIGNATURE_INVALID")
	}
	if strictJSON(payload, &m) != nil {
		return Manifest{}, errors.New("UPDATE_MANIFEST_INVALID")
	}
	return m, m.Validate(m.PublishedAt)
}

// Sign is used only by the owner's offline release tool and tests.
func Sign(m Manifest, key ed25519.PrivateKey, now time.Time) ([]byte, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("UPDATE_SIGNING_KEY_INVALID")
	}
	if err := m.Validate(now); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	signature := ed25519.Sign(key, append([]byte(domain), payload...))
	return json.Marshal(Envelope{base64.StdEncoding.EncodeToString(payload), base64.StdEncoding.EncodeToString(signature)})
}

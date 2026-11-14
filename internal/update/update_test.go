package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (Manifest, ed25519.PrivateKey, string) {
	t.Helper()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("valid package"))
	version := "0.3.0"
	p := Package{"windows-amd64", Filename("windows-amd64", version), "https://release.example/" + Filename("windows-amd64", version), 13, hex.EncodeToString(hash[:])}
	m := Manifest{Schema: 1, Sequence: 3, PublishedAt: time.Now().UTC().Add(-time.Hour), ExpiresAt: time.Now().UTC().Add(time.Hour), Current: Release{version, API{1, 0}, 1, []Package{p}}}
	previous := m.Current
	previous.Version = "0.2.0"
	previous.Packages = append([]Package{}, previous.Packages...)
	previous.Packages[0].Name = Filename("windows-amd64", previous.Version)
	previous.Packages[0].URL = "https://release.example/" + previous.Packages[0].Name
	m.Previous = &previous
	return m, key, hex.EncodeToString(public)
}

func signed(t *testing.T, m Manifest, key ed25519.PrivateKey) []byte {
	t.Helper()
	data, err := Sign(m, key, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLC011SignatureAndMetadata(t *testing.T) {
	m, key, public := fixture(t)
	data := signed(t, m, key)
	if _, err := Verify(data, public, time.Now()); err != nil {
		t.Fatal(err)
	}
	var e Envelope
	_ = json.Unmarshal(data, &e)
	e.Signature = strings.Repeat("A", 88)
	bad, _ := json.Marshal(e)
	if _, err := Verify(bad, public, time.Now()); err == nil {
		t.Fatal("accepted changed signature")
	}
	_, _, other := fixture(t)
	if _, err := Verify(data, other, time.Now()); err == nil {
		t.Fatal("accepted unpinned signer")
	}
	if _, err := Verify(data, public, time.Now().Add(2*time.Hour)); err == nil {
		t.Fatal("accepted expired metadata")
	}
	if _, err := VerifyHistorical(data, public); err != nil {
		t.Fatal("owner cannot reuse signed release", err)
	}
	if _, err := Verify(append(data, []byte(`{}`)...), public, time.Now()); err == nil {
		t.Fatal("accepted trailing JSON")
	}
	for _, badURL := range []string{"http://release.example/a", "https://user:secret@release.example/a", "https://release.example/a?token=x", "https://release.example/a#fragment"} {
		t.Run(badURL, func(t *testing.T) {
			altered := m
			altered.Current.Packages = append([]Package{}, m.Current.Packages...)
			altered.Current.Packages[0].URL = badURL
			if _, err := Sign(altered, key, time.Now()); err == nil {
				t.Fatal("signed unsafe URL")
			}
		})
	}
	for _, alter := range []func(*Manifest){
		func(m *Manifest) { m.Current.Packages[0].Name = "../setup.exe" },
		func(m *Manifest) { m.Current.Packages[0].Size = MaxPackage + 1 },
		func(m *Manifest) { m.Current.Packages = append(m.Current.Packages, m.Current.Packages[0]) },
		func(m *Manifest) { m.ExpiresAt = m.PublishedAt.Add(31 * 24 * time.Hour) },
		func(m *Manifest) { m.Previous.Version = m.Current.Version },
		func(m *Manifest) { m.PublishedAt = time.Now().Add(time.Hour) },
	} {
		value, k, _ := fixture(t)
		alter(&value)
		if _, err := Sign(value, k, time.Now()); err == nil {
			t.Fatal("signed invalid metadata")
		}
	}
}

func TestLC011DownloadAndExplicitRollback(t *testing.T) {
	m, key, public := fixture(t)
	var manifest []byte
	packageBody := "valid package"
	redirect := false
	interrupted := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed.json" {
			w.Write(manifest)
			return
		}
		if redirect {
			http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
			return
		}
		if interrupted {
			w.Header().Set("Content-Length", "13")
			fmt.Fprint(w, "short")
			return
		}
		fmt.Fprint(w, packageBody)
	}))
	defer server.Close()
	for _, r := range []*Release{&m.Current, m.Previous} {
		for i := range r.Packages {
			r.Packages[i].URL = server.URL + "/" + r.Packages[i].Name
		}
	}
	manifest = signed(t, m, key)
	state, cache := t.TempDir(), t.TempDir()
	c := New("windows-amd64", "0.2.0-dev", state, cache)
	defer c.Close()
	c.Feed, c.Key = server.URL+"/feed.json", public
	c.HTTP = server.Client()
	c.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx := context.Background()
	if _, err := c.Download(ctx, false, false); err == nil {
		t.Fatal("download without check")
	}
	if _, err := c.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Check(ctx); err != nil {
		t.Fatal("repeat check failed", err)
	}
	if _, err := c.Download(ctx, false, true); err == nil {
		t.Fatal("download during playback")
	}
	path, err := c.Download(ctx, false, false)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != packageBody {
		t.Fatal("wrong package")
	}
	previousPath, err := c.Download(ctx, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if previousPath == path || !strings.Contains(previousPath, "0.2.0") {
		t.Fatal("rollback not separate")
	}
	for _, body := range []string{"corrupt", strings.Repeat("x", 14), "bad package!!"} {
		packageBody = body
		if _, err := c.Download(ctx, false, false); err == nil {
			t.Fatal("corrupt package published")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("failed download appears ready")
		}
		if _, err := os.Stat(filepath.Join(cache, "updates", "current", "package.part")); !os.IsNotExist(err) {
			t.Fatal("partial remains")
		}
	}
	if _, err := os.Stat(previousPath); err != nil {
		t.Fatal("failed current removed rollback", err)
	}
	redirect = true
	if _, err := c.Download(ctx, false, false); err == nil {
		t.Fatal("followed redirect")
	}
	redirect = false
	packageBody = "valid package"
	interrupted = true
	if _, err := c.Download(ctx, false, false); err == nil || err.Error() != "UPDATE_DOWNLOAD_INTERRUPTED" {
		t.Fatal("interrupted transfer not detected", err)
	}
	interrupted = false
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Download(cancelled, false, false); err == nil {
		t.Fatal("cancelled download published")
	}
	if _, err := c.Download(ctx, false, false); err != nil {
		t.Fatal("retry failed", err)
	}
	// A new process retains the sequence watermark and denies feed downgrade.
	m.Sequence = 2
	manifest = signed(t, m, key)
	fresh := New(c.Platform, c.Version, state, cache)
	defer fresh.Close()
	fresh.HTTP = c.HTTP
	fresh.Feed, fresh.Key = c.Feed, c.Key
	if _, err := fresh.Check(ctx); err == nil || err.Error() != "UPDATE_REPLAY_DENIED" {
		t.Fatal("replayed metadata", err)
	}
	m.Sequence = 3
	m.Current.API.Minor = 1
	manifest = signed(t, m, key)
	if _, err := fresh.Check(ctx); err == nil || err.Error() != "UPDATE_REPLAY_DENIED" {
		t.Fatal("sequence reused", err)
	}
	m.Sequence = 4
	m.Current.SettingsSchema = 2
	manifest = signed(t, m, key)
	if _, err := fresh.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Download(ctx, false, false); err == nil || err.Error() != "UPDATE_SETTINGS_INCOMPATIBLE" {
		t.Fatal("incompatible settings allowed", err)
	}
	fresh.Version = "0.4.0"
	m.Sequence = 5
	m.Current.SettingsSchema = 1
	manifest = signed(t, m, key)
	if _, err := fresh.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Download(ctx, false, false); err == nil || err.Error() != "UPDATE_DOWNGRADE_DENIED" {
		t.Fatal("implicit downgrade allowed", err)
	}
	if _, err := fresh.Download(ctx, true, false); err != nil {
		t.Fatal("explicit rollback failed", err)
	}
}

func TestLC011UpdateStorageRejectsSymlinkRoot(t *testing.T) {
	dir, external := t.TempDir(), t.TempDir()
	marker := filepath.Join(external, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "updates")
	if err := os.Symlink(external, link); err != nil {
		t.Skip("symlink privilege unavailable")
	}
	if root, err := openDirectory(link); err == nil {
		root.Close()
		t.Fatal("followed a replaced update directory")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
		t.Fatal("changed external files")
	}
}

func TestLC011UnknownAndNoncanonicalMetadata(t *testing.T) {
	m, key, public := fixture(t)
	data := signed(t, m, key)
	var envelope Envelope
	_ = json.Unmarshal(data, &envelope)
	// Duplicate keys in either layer cannot be hidden behind JSON parser rules.
	data = append([]byte(`{"payload":"duplicate",`), data[1:]...)
	if _, err := Verify(data, public, time.Now()); err == nil {
		t.Fatal("accepted duplicate envelope key")
	}
	if _, err := Verify(make([]byte, MaxManifest+1), public, time.Now()); err == nil {
		t.Fatal("oversized metadata accepted")
	}
	for _, v := range []string{"01.0.0", "0.2", "dev", "1.0.0-beta", "1.0.4294967296"} {
		if _, err := Compare(v, "0.2.0"); err == nil {
			t.Fatal("bad version", v)
		}
	}
	if n, err := Compare("0.2.0-dev", "0.2.0"); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if _, err := openDirectory("relative"); err == nil {
		t.Fatal("relative update storage allowed")
	}
}

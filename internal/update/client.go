package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Client struct {
	Feed, Key, Platform, Version, State, Cache string
	HTTP                                       *http.Client
	Now                                        func() time.Time
	mu                                         sync.Mutex
	manifest                                   *Manifest
	envelope                                   []byte
}

func New(platform, version, state, cache string) *Client {
	return &Client{Feed: FeedURL, Key: PublicKey, Platform: platform, Version: version, State: state, Cache: cache, Now: time.Now,
		HTTP: &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 10 * time.Second}, CheckRedirect: githubAssetRedirect}}
}

func (c *Client) Close() { c.HTTP.CloseIdleConnections() }

// Directory is opened without following a final symlink, then all operations
// use Root to confine even a replaced entry. No publisher path is used locally.
func openDirectory(path string) (*os.Root, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	return root, nil
}

func (c *Client) get(ctx context.Context, url string, limit int64) (*http.Response, error) {
	if !HTTPS(url) {
		return nil, errors.New("UPDATE_URL_INVALID")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, errors.New("UPDATE_URL_INVALID")
	}
	req.Header.Set("Accept-Encoding", "identity")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("UPDATE_CONNECTION_FAILED")
	}
	if response.StatusCode != http.StatusOK || response.ContentLength > limit || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		response.Body.Close()
		return nil, errors.New("UPDATE_DOWNLOAD_INVALID")
	}
	return response, nil
}

type checkpoint struct {
	Sequence uint64 `json:"sequence"`
	Digest   string `json:"digest"`
}

func (c *Client) Check(ctx context.Context) (Manifest, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.manifest = nil
	c.envelope = nil
	if !HTTPS(c.Feed) || len(c.Key) != 64 {
		return Manifest{}, errors.New("UPDATE_NOT_CONFIGURED")
	}
	response, err := c.get(ctx, c.Feed, MaxManifest)
	if err != nil {
		return Manifest{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxManifest+1))
	if err != nil {
		return Manifest{}, errors.New("UPDATE_DOWNLOAD_INTERRUPTED")
	}
	m, err := Verify(data, c.Key, c.Now())
	if err != nil {
		return Manifest{}, err
	}
	root, err := openDirectory(filepath.Join(c.State, "updates"))
	if err != nil {
		return Manifest{}, err
	}
	defer root.Close()
	var saved checkpoint
	old, err := root.ReadFile("checkpoint.json")
	if err != nil && !os.IsNotExist(err) || err == nil && (len(old) > 1024 || strictJSON(old, &saved) != nil) {
		return Manifest{}, errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	sum := sha256.Sum256(data)
	next := checkpoint{m.Sequence, hex.EncodeToString(sum[:])}
	if m.Sequence < saved.Sequence || m.Sequence == saved.Sequence && next.Digest != saved.Digest {
		return Manifest{}, errors.New("UPDATE_REPLAY_DENIED")
	}
	_ = root.Remove("checkpoint.part")
	file, err := root.OpenFile("checkpoint.part", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Manifest{}, errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	defer root.Remove("checkpoint.part")
	_, writeErr := file.Write(mustJSON(next))
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || root.Rename("checkpoint.part", "checkpoint.json") != nil {
		return Manifest{}, errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	c.manifest = &m
	c.envelope = append([]byte(nil), data...)
	return m, nil
}

func mustJSON(value any) []byte { data, _ := json.Marshal(value); return data }

func (c *Client) selected(previous bool) (Release, Package, error) {
	if c.manifest == nil {
		return Release{}, Package{}, errors.New("UPDATE_CHECK_REQUIRED")
	}
	if err := c.manifest.Validate(c.Now()); err != nil {
		return Release{}, Package{}, err
	}
	r := c.manifest.Current
	if previous {
		if c.manifest.Previous == nil {
			return r, Package{}, errors.New("UPDATE_ROLLBACK_UNAVAILABLE")
		}
		r = *c.manifest.Previous
	} else {
		order, err := Compare(r.Version, c.Version)
		if err != nil || order < 0 {
			return r, Package{}, errors.New("UPDATE_DOWNGRADE_DENIED")
		}
	}
	if r.SettingsSchema != SettingsSchema || r.API.Major != 1 {
		return r, Package{}, errors.New("UPDATE_SETTINGS_INCOMPATIBLE")
	}
	for _, p := range r.Packages {
		if p.Platform == c.Platform {
			return r, p, nil
		}
	}
	return r, Package{}, errors.New("UPDATE_PLATFORM_UNSUPPORTED")
}

// Download publishes a file only after exact size and hash verification. A
// failed/interrupted transfer cannot leave an apparently ready installer.
// Repeating the user action starts a clean transfer, never an unchecked resume.
func (c *Client) Download(ctx context.Context, previous bool, active bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if active {
		return "", errors.New("UPDATE_PLAYBACK_ACTIVE")
	}
	_, p, err := c.selected(previous)
	if err != nil {
		return "", err
	}
	base := filepath.Join(c.Cache, "updates")
	root, err := openDirectory(base)
	if err != nil {
		return "", err
	}
	defer root.Close()
	slot := "current"
	if previous {
		slot = "previous"
	}
	// Both targets are fixed children of our private update directory.
	if err := root.RemoveAll(slot); err != nil {
		return "", errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	if err := root.Mkdir(slot, 0700); err != nil {
		return "", errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	dir, err := root.OpenRoot(slot)
	if err != nil {
		return "", errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	defer dir.Close()
	file, err := dir.OpenFile("package.part", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	defer dir.Remove("package.part")
	response, err := c.get(ctx, p.URL, p.Size)
	if err != nil {
		file.Close()
		return "", err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, p.Size+1))
	response.Body.Close()
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil || ctx.Err() != nil {
		return "", errors.New("UPDATE_DOWNLOAD_INTERRUPTED")
	}
	if syncErr != nil || closeErr != nil {
		return "", errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	if n != p.Size || hex.EncodeToString(hash.Sum(nil)) != p.SHA256 {
		return "", errors.New("UPDATE_PACKAGE_INVALID")
	}
	if err := dir.WriteFile("manifest.json", c.envelope, 0600); err != nil {
		return "", errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	if err := dir.Rename("package.part", p.Name); err != nil {
		return "", errors.New("UPDATE_STORAGE_UNAVAILABLE")
	}
	return filepath.Join(base, slot, p.Name), nil
}

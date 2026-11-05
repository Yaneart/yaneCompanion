// Development tool only; never included in the user's runtime package.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type source struct{ Name, URL, SHA256 string }

func verify(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return fmt.Errorf("hash mismatch: %s", filepath.Base(path))
	}
	return nil
}
func run() error {
	data, err := os.ReadFile("release/dependencies.lock.json")
	if err != nil {
		return err
	}
	var lock struct{ Sources []source }
	if err := json.Unmarshal(data, &lock); err != nil {
		return err
	}
	if err := os.MkdirAll("artifacts", 0700); err != nil {
		return err
	}
	client := &http.Client{Timeout: 180 * time.Second, CheckRedirect: func(r *http.Request, _ []*http.Request) error {
		if r.URL.Scheme != "https" {
			return fmt.Errorf("HTTPS required")
		}
		return nil
	}}
	for _, item := range lock.Sources {
		if item.Name != filepath.Base(item.Name) || !strings.HasPrefix(item.URL, "https://") {
			return fmt.Errorf("invalid source")
		}
		path := filepath.Join("artifacts", item.Name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := download(client, item, path); err != nil {
				return err
			}
		}
		if err := verify(path, item.SHA256); err != nil {
			return err
		}
		fmt.Println("Verified", item.Name)
	}
	return nil
}
func download(client *http.Client, item source, path string) error {
	response, err := client.Get(item.URL)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("download failed: %s", item.Name)
	}
	partial := path + ".part"
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, 256<<20))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := verify(partial, item.SHA256); err != nil {
		return err
	}
	return os.Rename(partial, path)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

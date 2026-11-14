package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yanecompanion/internal/update"
)

func TestLC011OwnerSigningVerifiesActualPackages(t *testing.T) {
	root := t.TempDir()
	public, key, _ := ed25519.GenerateKey(rand.Reader)
	encoded := hex.EncodeToString(public)
	name := update.Filename("windows-amd64", "0.3.0")
	body := []byte("package fixture")
	sum := sha256.Sum256(body)
	m := update.Manifest{Schema: 1, Sequence: 1, PublishedAt: time.Now().UTC().Add(-time.Hour), ExpiresAt: time.Now().UTC().Add(time.Hour), Current: update.Release{Version: "0.3.0", API: update.API{Major: 1}, SettingsSchema: 1, Packages: []update.Package{{Platform: "windows-amd64", Name: name, URL: "https://release.example/" + name, Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}}}}
	input := filepath.Join(root, "input.json")
	out := filepath.Join(root, "release.json")
	secret := filepath.Join(root, "owner.seed")
	data, _ := json.Marshal(m)
	os.WriteFile(input, data, 0600)
	os.WriteFile(secret, []byte(hex.EncodeToString(key.Seed())), 0600)
	os.WriteFile(filepath.Join(root, name), body, 0600)
	invoke := func(args ...string) error {
		oldFlags, oldArgs := flag.CommandLine, os.Args
		defer func() { flag.CommandLine, os.Args = oldFlags, oldArgs }()
		flag.CommandLine = flag.NewFlagSet("release-manifest", flag.ContinueOnError)
		flag.CommandLine.SetOutput(io.Discard)
		os.Args = append([]string{"release-manifest"}, args...)
		return run()
	}
	args := []string{"-manifest", input, "-packages", root, "-key-file", secret, "-public-key", encoded, "-out", out}
	if err := invoke(args...); err != nil {
		t.Fatal(err)
	}
	if err := invoke("-manifest", out, "-public-key", encoded, "-packages", root); err != nil {
		t.Fatal(err)
	}
	if err := invoke(args...); err != nil {
		t.Fatal("atomic replacement failed", err)
	}
	before, _ := os.ReadFile(out)
	os.WriteFile(filepath.Join(root, name), []byte("broken"), 0600)
	if err := invoke(args...); err == nil {
		t.Fatal("signed corrupt package")
	}
	after, _ := os.ReadFile(out)
	if string(after) != string(before) {
		t.Fatal("failed signing replaced good output")
	}
	if err := invoke("-manifest", out, "-public-key", encoded, "-packages", root); err == nil {
		t.Fatal("verified corrupt package")
	}
	data, _ = os.ReadFile(secret)
	if string(data) != hex.EncodeToString(key.Seed()) {
		t.Fatal("key changed")
	}
}

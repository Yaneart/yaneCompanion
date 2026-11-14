// Offline owner tool. Private keys are read from a separate file and never
// printed, copied into a bundle or accepted from the network.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yanecompanion/internal/update"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	input := flag.String("manifest", "", "manifest JSON (unsigned input when signing, signed envelope when verifying)")
	keyFile := flag.String("key-file", "", "owner's external file: hex-encoded Ed25519 32-byte seed")
	public := flag.String("public-key", "", "pinned hex-encoded public key")
	historical := flag.Bool("historical", false, "verify an archived signature without using it as a live update feed")
	packages := flag.String("packages", "", "directory containing the exact packages referenced in the input")
	out := flag.String("out", "", "signed output path; empty means verification only")
	flag.Parse()
	if flag.NArg() != 0 || *input == "" || len(*public) != 64 {
		return errors.New("RELEASE_ARGUMENTS_INVALID")
	}
	data, err := readSmall(*input, update.MaxManifest)
	if err != nil {
		return errors.New("RELEASE_INPUT_UNAVAILABLE")
	}
	now := time.Now().UTC()
	var manifest update.Manifest
	if *out == "" {
		if *historical {
			manifest, err = update.VerifyHistorical(data, *public)
		} else {
			manifest, err = update.Verify(data, *public, now)
		}
	} else {
		if *historical {
			return errors.New("RELEASE_ARGUMENTS_INVALID")
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&manifest) != nil || decoder.Decode(new(any)) != io.EOF {
			return errors.New("RELEASE_INPUT_INVALID")
		}
		err = manifest.Validate(now)
	}
	if err != nil {
		return err
	}
	if *packages == "" && *out != "" {
		return errors.New("RELEASE_PACKAGES_REQUIRED")
	}
	if *packages != "" {
		root, err := os.OpenRoot(*packages)
		if err != nil {
			return errors.New("RELEASE_PACKAGES_UNAVAILABLE")
		}
		defer root.Close()
		all := []update.Release{manifest.Current}
		if manifest.Previous != nil {
			all = append(all, *manifest.Previous)
		}
		for _, release := range all {
			for _, p := range release.Packages {
				file, err := root.Open(p.Name)
				if err != nil {
					return errors.New("RELEASE_PACKAGE_MISSING")
				}
				info, statErr := file.Stat()
				if statErr != nil || !info.Mode().IsRegular() || info.Size() != p.Size {
					file.Close()
					return errors.New("RELEASE_PACKAGE_INVALID")
				}
				hash := sha256.New()
				_, err = io.Copy(hash, file)
				file.Close()
				if err != nil || hex.EncodeToString(hash.Sum(nil)) != p.SHA256 {
					return errors.New("RELEASE_PACKAGE_INVALID")
				}
			}
		}
	}
	if *out != "" {
		secret, err := readSmall(*keyFile, 256)
		if err != nil {
			return errors.New("RELEASE_KEY_UNAVAILABLE")
		}
		seed, err := hex.DecodeString(strings.TrimSpace(string(secret)))
		if err != nil || len(seed) != ed25519.SeedSize {
			return errors.New("RELEASE_KEY_INVALID")
		}
		key := ed25519.NewKeyFromSeed(seed)
		if hex.EncodeToString(key.Public().(ed25519.PublicKey)) != *public {
			return errors.New("RELEASE_KEY_MISMATCH")
		}
		signed, err := update.Sign(manifest, key, now)
		if err != nil {
			return err
		}
		outputPath, _ := filepath.Abs(*out)
		inputPath, _ := filepath.Abs(*input)
		secretPath, _ := filepath.Abs(*keyFile)
		if strings.EqualFold(outputPath, secretPath) || strings.EqualFold(outputPath, inputPath) {
			return errors.New("RELEASE_OUTPUT_INVALID")
		}
		file, err := os.CreateTemp(filepath.Dir(outputPath), ".manifest-")
		if err != nil {
			return errors.New("RELEASE_OUTPUT_UNAVAILABLE")
		}
		defer os.Remove(file.Name())
		_, writeErr := file.Write(append(signed, '\n'))
		syncErr := file.Sync()
		closeErr := file.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil || os.Rename(file.Name(), outputPath) != nil {
			return errors.New("RELEASE_OUTPUT_UNAVAILABLE")
		}
	}
	fmt.Println("Release manifest verified; publication remains manual.")
	return nil
}

func readSmall(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("INPUT_TOO_LARGE")
	}
	return data, nil
}

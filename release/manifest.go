package release

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed dependencies.lock.json
var locked []byte

const Version = "0.1.0-dev"
const EngineVersion = "MatriX.148-companion.1"
const MediaVersion = "9.0.2"

type Artifact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Platforms map[string][]Artifact `json:"platforms"`
}
type Bundle struct{ Engine, FFmpeg, FFprobe string }

func Verify(directory string) (Bundle, error) {
	var manifest Manifest
	if json.Unmarshal(locked, &manifest) != nil {
		return Bundle{}, errors.New("INVALID_LOCK")
	}
	artifacts, ok := manifest.Platforms[runtime.GOOS+"-"+runtime.GOARCH]
	if !ok || len(artifacts) != 3 {
		return Bundle{}, errors.New("UNSUPPORTED_PLATFORM")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return Bundle{}, errors.New("BUNDLE_MISSING")
	}
	defer root.Close()
	paths := make(map[string]string)
	for _, artifact := range artifacts {
		if filepath.Base(artifact.Name) != artifact.Name || len(artifact.SHA256) != 64 {
			return Bundle{}, errors.New("INVALID_LOCK")
		}
		file, err := root.Open(filepath.Join("tools", artifact.Name))
		if err != nil {
			return Bundle{}, errors.New("BUNDLE_MISSING")
		}
		hash := sha256.New()
		_, err = io.Copy(hash, file)
		file.Close()
		if err != nil || hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
			return Bundle{}, errors.New("BUNDLE_HASH_MISMATCH")
		}
		paths[artifact.Name] = filepath.Join(directory, "tools", artifact.Name)
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	bundle := Bundle{paths["torrserver"+suffix], paths["ffmpeg"+suffix], paths["ffprobe"+suffix]}
	if bundle.Engine == "" || bundle.FFmpeg == "" || bundle.FFprobe == "" {
		return Bundle{}, errors.New("INVALID_LOCK")
	}
	return bundle, nil
}

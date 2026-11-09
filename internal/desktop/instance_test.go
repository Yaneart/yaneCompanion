package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yanecompanion/internal/lifecycle"
)

func TestDesktopInstanceLifetimeAndActivationProof(t *testing.T) {
	dir := t.TempDir()
	instance, err := OpenInstance(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(instance.Close)
	if _, err := OpenInstance(dir); !errors.Is(err, lifecycle.ErrRunning) {
		t.Fatal("GUI lock did not survive independently of runtime", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "desktop-control.json"))
	if err != nil {
		t.Fatal(err)
	}
	var proof activation
	if err := json.Unmarshal(data, &proof); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	for _, input := range []struct{ method, path, origin, token string }{{"POST", "/show", "https://watch.example", proof.Token}, {"POST", "/show", "", "wrong"}, {"GET", "/show", "", proof.Token}, {"POST", "/decision", "", proof.Token}, {"POST", "/show?code=123456", "", proof.Token}} {
		r, _ := http.NewRequest(input.method, "http://"+proof.Address+input.path, nil)
		if input.origin != "" {
			r.Header.Set("Origin", input.origin)
		}
		r.Header.Set("Authorization", "Bearer "+input.token)
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode < 400 || response.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("untrusted activation accepted", input.path, response.StatusCode)
		}
	}
	select {
	case <-instance.Show:
		t.Fatal("untrusted caller raised window")
	default:
	}
	if err := Activate(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	select {
	case <-instance.Show:
	case <-time.After(time.Second):
		t.Fatal("valid activation was lost")
	}
	instance.Close()
	if _, err := os.Stat(filepath.Join(dir, "desktop-control.json")); !os.IsNotExist(err) {
		t.Fatal("activation proof survived GUI exit")
	}
	other, err := OpenInstance(dir)
	if err != nil {
		t.Fatal("GUI lock not released", err)
	}
	other.Close()
}

func TestMalformedDesktopActivationCannotPanicOrLeaveLoopback(t *testing.T) {
	for _, address := range []string{"127.0.0.1:+123", "127.0.0.1:00123", "127.0.0.1:0", "127.0.0.1:65536", "localhost:123", "192.0.2.1:123", "127.0.0.1:123/path"} {
		proof := activation{Address: address, Token: strings.Repeat("a", 64)}
		if validActivation(proof) {
			t.Fatalf("accepted noncanonical activation address %q", address)
		}
		dir := t.TempDir()
		if err := writePrivateJSON(filepath.Join(dir, "desktop-control.json"), proof); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		if err := Activate(ctx, dir); err == nil {
			t.Fatal("invalid activation succeeded")
		}
		cancel()
	}
	if !validActivation(activation{Address: "127.0.0.1:123", Token: strings.Repeat("a", 64)}) {
		t.Fatal("canonical proof refused")
	}
}

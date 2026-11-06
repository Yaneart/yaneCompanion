package engine

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yanecompanion/release"
)

// A known torrent is essential: stock MatriX.148 rejects an unknown hash while
// allowing unauthenticated playback of a torrent that has already been added.
func TestBundledEngineRawAuth(t *testing.T) {
	executable := os.Getenv("YANE_TEST_EXECUTABLE")
	if executable == "" {
		t.Skip("set YANE_TEST_EXECUTABLE to a built, bundled host")
	}
	bundle, err := release.Verify(filepath.Dir(executable))
	if err != nil {
		t.Fatal(err)
	}
	e, err := Start(context.Background(), bundle.Engine, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.client.Timeout = 3 * time.Second
	piece := sha1.Sum([]byte{0})
	info := "d6:lengthi1e4:name11:fixture.mp412:piece lengthi16384e6:pieces20:" + string(piece[:]) + "e"
	digest := sha1.Sum([]byte(info))
	hash := hex.EncodeToString(digest[:])
	var upload bytes.Buffer
	form := multipart.NewWriter(&upload)
	file, err := form.CreateFormFile("file", "fixture.torrent")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(file, "d4:info"+info+"e")
	_ = form.Close()
	r, _ := http.NewRequest("POST", e.address+"/torrent/upload", &upload)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.SetBasicAuth("companion", e.password)
	response, err := e.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(data), hash) || !strings.Contains(string(data), "fixture.mp4") {
		t.Fatalf("metadata fixture upload: %d %s", response.StatusCode, data)
	}
	request := func(method, path, body string, authenticated bool, origin string) (int, string) {
		t.Helper()
		r, err := http.NewRequest(method, e.address+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		if authenticated {
			r.SetBasicAuth("companion", e.password)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		response, err := e.client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return response.StatusCode, string(data)
	}
	if status, body := request("POST", "/torrents", `{"action":"list"}`, true, ""); status != 200 || !strings.Contains(body, hash) {
		t.Fatalf("known torrent missing: %d %s", status, body)
	}
	for _, path := range []string{
		"/stream?link=" + hash + "&index=1&play",
		"/stream/fixture.mp4?link=" + hash + "&index=1&m3u",
		"/play/" + hash + "/1",
		"/m3u?link=" + hash,
	} {
		if status, _ := request("GET", path, "", false, ""); status != 401 {
			t.Errorf("raw route %s: %d", path, status)
		}
		if status, _ := request("HEAD", path, "", false, ""); status != 401 {
			t.Errorf("raw HEAD route %s: %d", path, status)
		}
		if status, _ := request("GET", path, "", true, "https://watch.example"); status != 403 {
			t.Errorf("browser raw route %s: %d", path, status)
		}
	}
	// A stream request queued before stop may reach the server after Drop.
	// Reading the removed hash must never perform torrent admission again.
	if status, _ := request("GET", "/stream?link="+hash+"&index=1&m3u", "", true, ""); status != http.StatusOK {
		t.Fatalf("active torrent playlist: expected 200, got %d", status)
	}
	if err := e.Drop(context.Background(), hash); err != nil {
		t.Fatal(err)
	}
	if status, _ := request("GET", "/stream?link="+hash+"&index=1&play", "", true, ""); status != http.StatusNotFound {
		t.Fatalf("removed torrent stream: expected 404, got %d", status)
	}
	if status, body := request("POST", "/torrents", `{"action":"list"}`, true, ""); status != 200 || strings.Contains(body, hash) {
		t.Fatalf("stream recreated removed torrent: %d %s", status, body)
	}

}

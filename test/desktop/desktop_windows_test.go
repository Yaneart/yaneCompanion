package desktop_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"yanecompanion/internal/config"
)

var user = windows.NewLazySystemDLL("user32.dll")

func ui(name string, args ...uintptr) uintptr {
	result, _, _ := user.NewProc(name).Call(args...)
	return result
}
func str(value string) uintptr { return uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(value))) }

func TestInstalledDesktopConsentAndLifecycle(t *testing.T) {
	exe := os.Getenv("YANE_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("opt in with a bundled executable; needs a Windows desktop")
	}
	root := filepath.Join(t.TempDir(), "Проверка desktop with spaces")
	env := append(os.Environ(), "LOCALAPPDATA="+root, "PATH=")
	start := func() *exec.Cmd {
		cmd := exec.Command(exe, "desktop", "--hidden")
		cmd.Env = env
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		return cmd
	}
	cmd := start()
	var credential struct {
		Token string `json:"token"`
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	call := func(method, path, origin, token string, body any) (int, []byte) {
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		r, _ := http.NewRequest(method, "http://"+config.Address+path, bytes.NewReader(data))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		response, err := client.Do(r)
		if err != nil {
			return 0, nil
		}
		defer response.Body.Close()
		data, _ = io.ReadAll(response.Body)
		return response.StatusCode, data
	}
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("desktop condition timed out")
	}
	wait(func() bool {
		data, err := os.ReadFile(filepath.Join(root, "yaneCompanion", "control.json"))
		if err != nil {
			return false
		}
		return json.Unmarshal(data, &credential) == nil && credential.Token != ""
	})
	var window uintptr
	wait(func() bool {
		window = ui("FindWindowW", str("yaneCompanionDesktop"), 0)
		return window != 0 && ui("GetDlgItem", window, 102) != 0
	})
	click := func(id uintptr) { ui("SendMessageW", window, 0x111, id, 0) }
	ui("SendMessageW", ui("GetDlgItem", window, 101), 0xc, 0, str("https://watch.example"))
	click(102)
	wait(func() bool { code, _ := call("GET", "/hello", "https://watch.example", "", nil); return code == 200 })
	code, data := call("POST", "/v1/pairings", "https://watch.example", "", map[string]any{})
	if code != 201 {
		t.Fatalf("pairing: %d %s", code, data)
	}
	var request struct {
		ID   string `json:"requestId"`
		Poll string `json:"pollToken"`
		Code string `json:"code"`
	}
	_ = json.Unmarshal(data, &request)
	wait(func() bool {
		value := make([]uint16, 2048)
		ui("SendMessageW", ui("GetDlgItem", window, 202), 0xd, uintptr(len(value)), uintptr(unsafe.Pointer(&value[0])))
		return bytes.Contains([]byte(windows.UTF16ToString(value)), []byte(request.Code))
	})
	click(105)
	code, data = call("GET", "/v1/pairings/"+request.ID, "https://watch.example", request.Poll, nil)
	var result struct {
		State string `json:"state"`
		Token string `json:"pairingToken"`
	}
	_ = json.Unmarshal(data, &result)
	if code != 200 || result.State != "approved" || len(result.Token) != 64 {
		t.Fatalf("native consent did not approve: %d %s", code, data)
	}
	// Window close hides the UI while the host and its saved grant remain usable.
	ui("SendMessageW", window, 0x10, 0, 0)
	if code, _ := call("GET", "/v1/pairings/current", "https://watch.example", result.Token, nil); code != 200 {
		t.Fatal("hiding stopped the host")
	}
	second := start()
	if err := second.Wait(); err != nil {
		t.Fatal("second desktop instance did not activate the original", err)
	}
	if code, _ := call("GET", "/local/status", "", credential.Token, nil); code != 200 {
		t.Fatal("second start lost first instance")
	}
	click(110)
	if err := cmd.Wait(); err != nil {
		t.Fatal("desktop exit", err)
	}
	if code, _ := call("GET", "/hello", "https://watch.example", "", nil); code != 0 {
		t.Fatal("Exit left a listener")
	}
	cmd = start()
	wait(func() bool {
		code, _ := call("GET", "/v1/pairings/current", "https://watch.example", result.Token, nil)
		return code == 200
	})
	window = ui("FindWindowW", str("yaneCompanionDesktop"), 0)
	// Revocation in the native UI reaches the same store used by the HTTP API.
	wait(func() bool { return ui("SendMessageW", ui("GetDlgItem", window, 107), 0x18b, 0, 0) > 0 })
	ui("SendMessageW", ui("GetDlgItem", window, 107), 0x186, 0, 0)
	click(108)
	if code, _ := call("GET", "/v1/pairings/current", "https://watch.example", result.Token, nil); code != 401 {
		t.Fatal("native revoke did not invalidate grant")
	}
	click(110)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestDesktopStartupFailureRemainsSingle(t *testing.T) {
	source := os.Getenv("YANE_TEST_EXECUTABLE")
	if source == "" {
		t.Skip("opt in with bundled executable")
	}
	root := t.TempDir()
	exe := filepath.Join(root, "yanecompanion.exe")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(exe, data, 0700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(root, "profile")
	start := func() *exec.Cmd {
		cmd := exec.Command(exe, "desktop", "--hidden")
		cmd.Env = append(os.Environ(), "LOCALAPPDATA="+profile, "PATH=")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		return cmd
	}
	first := start()
	deadline := time.Now().Add(10 * time.Second)
	var window uintptr
	foundError := false
	for time.Now().Before(deadline) {
		window = ui("FindWindowW", str("yaneCompanionDesktop"), 0)
		value := make([]uint16, 2048)
		if window != 0 {
			ui("SendMessageW", ui("GetDlgItem", window, 201), 0xd, uintptr(len(value)), uintptr(unsafe.Pointer(&value[0])))
			if bytes.Contains([]byte(windows.UTF16ToString(value)), []byte("BUNDLE_MISSING")) {
				foundError = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !foundError {
		t.Fatal("startup error UI not created")
	}
	second := start()
	secondDone := make(chan error, 1)
	go func() { secondDone <- second.Wait() }()
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal("second start did not activate error window", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second startup-error GUI remained alive")
	}
	if _, err := os.Stat(filepath.Join(profile, "yaneCompanion", "desktop-control.json")); err != nil {
		t.Fatal("GUI activation lifetime ended with runtime", err)
	}
	ui("SendMessageW", window, 0x111, 110, 0)
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.Wait() }()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal("closing visible startup error failed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing startup error did not exit")
	}
}

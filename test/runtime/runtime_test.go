package runtime_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yanecompanion/internal/config"
)

func bundle(t *testing.T) string {
	t.Helper()
	path := os.Getenv("YANE_TEST_EXECUTABLE")
	if path == "" {
		t.Skip("set YANE_TEST_EXECUTABLE to a built, bundled host")
	}
	return path
}

func isolatedEnv(t *testing.T) ([]string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state with spaces \u041f\u0440\u043e\u0432\u0435\u0440\u043a\u0430")
	env := append(os.Environ(), "LOCALAPPDATA="+root, "XDG_STATE_HOME="+root, "XDG_CONFIG_HOME="+root, "XDG_CACHE_HOME="+root, "PATH=")
	return env, root
}

func cli(exe string, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = env
	return cmd.CombinedOutput()
}

type host struct {
	cmd    *exec.Cmd
	done   chan struct{}
	err    error
	output bytes.Buffer
}

func (h *host) wait(t *testing.T) {
	t.Helper()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("host did not exit")
	}
}

func start(t *testing.T, exe string, env []string) *host {
	t.Helper()
	cmd := exec.Command(exe, "run", "--origin", "https://watch.example")
	cmd.Env = env
	h := &host{cmd: cmd, done: make(chan struct{})}
	cmd.Stdout = &h.output
	cmd.Stderr = &h.output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { h.err = cmd.Wait(); close(h.done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
		}
	})
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := cli(exe, env, "status"); err == nil {
			return h
		}
		select {
		case <-h.done:
			t.Fatalf("host exited: %v %s", h.err, h.output.String())
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("host did not become ready")
	return nil
}

func waitStopped(t *testing.T, exe string, env []string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := cli(exe, env, "status"); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("host did not stop")
}

func TestRuntimeLifecycle(t *testing.T) {
	exe := bundle(t)
	env, root := isolatedEnv(t)
	first := start(t, exe, env)
	if out, err := cli(exe, env, "run"); err == nil || !strings.Contains(string(out), "ALREADY_RUNNING") {
		t.Fatalf("duplicate: %v %s", err, out)
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	for _, tc := range []struct {
		method, path, origin string
		status               int
	}{{"GET", "/hello", "https://watch.example", 200}, {"GET", "/hello", "https://evil.example", 403}, {"POST", "/v1/sessions", "https://watch.example", 401}, {"POST", "/local/exit", "https://watch.example", 403}} {
		req, _ := http.NewRequest(tc.method, "http://"+config.Address+tc.path, nil)
		req.Header.Set("Origin", tc.origin)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if tc.path == "/hello" && res.StatusCode == 200 && (!strings.Contains(string(data), "torrent-admission.v1") || !strings.Contains(string(data), "sessions.v1") || !strings.Contains(string(data), "hls.mse") || !strings.Contains(string(data), "timeline.windowed-vod")) {
			t.Fatal("hello advertises incorrect admission/playback capabilities")
		}
		if res.StatusCode != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, res.StatusCode, data)
		}
	}
	if out, err := cli(exe, env, "exit"); err != nil {
		t.Fatalf("exit: %v %s", err, out)
	}
	waitStopped(t, exe, env)
	first.wait(t)
	if first.err != nil {
		t.Fatalf("unclean exit: %v %s", first.err, first.output.String())
	}
	second := start(t, exe, env)
	if err := second.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitStopped(t, exe, env)
	second.wait(t)
	// A crash leaves a marked run directory. A fresh boot invalidates credentials,
	// removes only that run and obtains the released OS lock.
	third := start(t, exe, env)
	var marked int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), "run-") {
			marked++
		}
		return nil
	})
	if err != nil || marked != 1 {
		t.Fatalf("crash cleanup: runs=%d err=%v", marked, err)
	}
	if out, err := cli(exe, env, "exit"); err != nil {
		t.Fatalf("exit after recovery: %v %s", err, out)
	}
	waitStopped(t, exe, env)
	third.wait(t)
}

func TestBusyPort(t *testing.T) {
	exe := bundle(t)
	env, _ := isolatedEnv(t)
	listener, err := net.Listen("tcp4", config.Address)
	if err != nil {
		t.Fatal("test port unavailable:", err)
	}
	defer listener.Close()
	out, err := cli(exe, env, "run")
	if err == nil || !strings.Contains(string(out), "PORT_IN_USE") {
		t.Fatalf("busy port: %v %s", err, out)
	}
}

func TestNoCheckoutRequired(t *testing.T) {
	exe := bundle(t)
	env, _ := isolatedEnv(t)
	directory := filepath.Join(t.TempDir(), "standalone bundle \u0412\u0438\u0434\u0435\u043e")
	if err := os.MkdirAll(filepath.Join(directory, "tools"), 0700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(exe), "tools"))
	if err != nil {
		t.Fatal(err)
	}
	files := []string{filepath.Base(exe)}
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, filepath.Join("tools", entry.Name()))
		}
	}
	for _, name := range files {
		if err := copyExecutable(filepath.Join(filepath.Dir(exe), name), filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	standalone := filepath.Join(directory, filepath.Base(exe))
	host := start(t, standalone, env)
	if out, err := cli(standalone, env, "exit"); err != nil {
		t.Fatal(fmt.Sprint(err), string(out))
	}
	host.wait(t)
}

func copyExecutable(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

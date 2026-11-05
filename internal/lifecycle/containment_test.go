package lifecycle

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"yanecompanion/release"
)

func TestContainmentHelper(t *testing.T) {
	if os.Getenv("YANE_CONTAINMENT_HELPER") != "1" {
		return
	}
	if err := ContainHost(); err != nil {
		t.Fatal(err)
	}
	bundle, err := release.Verify(filepath.Dir(os.Getenv("YANE_TEST_EXECUTABLE")))
	if err != nil {
		t.Fatal(err)
	}
	input := os.Getenv("YANE_CONTAINMENT_INPUT")
	worker, err := Start(exec.Command(bundle.FFmpeg, "-nostdin", "-v", "error", "-stream_loop", "-1", "-re", "-f", "s16le", "-ar", "48000", "-ac", "1", "-i", input, "-f", "null", "-"))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("WORKER", worker.PID())
	select {
	case <-worker.Done():
		t.Fatal("worker exited prematurely")
	case <-time.After(time.Minute):
		worker.Stop()
	}
}

func TestCrashKillsMediaWorker(t *testing.T) {
	if os.Getenv("YANE_TEST_EXECUTABLE") == "" {
		t.Skip("requires a built bundle")
	}
	input := filepath.Join(t.TempDir(), "silence.pcm")
	if err := os.WriteFile(input, make([]byte, 96000), 0600); err != nil {
		t.Fatal(err)
	}
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host := exec.CommandContext(ctx, testExecutable, "-test.run=^TestContainmentHelper$")
	host.Env = append(os.Environ(), "YANE_CONTAINMENT_HELPER=1", "YANE_CONTAINMENT_INPUT="+input)
	stdout, err := host.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	host.Stderr = os.Stderr
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Process.Kill() })
	scanner := bufio.NewScanner(stdout)
	pid := 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "WORKER" {
			pid, _ = strconv.Atoi(fields[1])
			break
		}
	}
	if pid == 0 {
		_ = host.Wait()
		t.Fatal("helper did not start media worker")
	}
	if !processAlive(pid) {
		t.Fatal("worker was not running before crash")
	}
	if err := host.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = host.Wait()
	deadline := time.Now().Add(3 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pid) {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
		t.Fatal("orphan FFmpeg survived host crash")
	}
}

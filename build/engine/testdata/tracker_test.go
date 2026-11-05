package tracker

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/companionguard"
)

func TestCompanionTrackerEgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("tracker request reached a local service")
	}))
	defer server.Close()
	for _, address := range []string{server.URL, "http://localhost:" + strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port)} {
		_, err := (Announce{TrackerUrl: address + "/announce"}).Do()
		if !errors.Is(err, companionguard.ErrDenied) {
			t.Fatalf("HTTP tracker %s: %v", address, err)
		}
	}
	for _, address := range []string{"udp://127.0.0.1:6969", "udp://1.1.1.1:6969", "udp6://[::1]:6969"} {
		_, err := (Announce{TrackerUrl: address}).Do()
		if err == nil || !strings.Contains(err.Error(), "COMPANION_UDP_DISABLED") {
			t.Fatalf("UDP tracker %s: %v", address, err)
		}
	}
}

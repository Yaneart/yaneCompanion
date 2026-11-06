package tracker

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	for _, address := range []string{"udp://127.0.0.1:6969", "udp://localhost:6969", "udp://10.0.0.1:6969", "udp://169.254.169.254:80", "udp://224.0.0.1:6969", "udp6://[::1]:6969", "udp://[::ffff:127.0.0.1]:6969"} {
		_, err := (Announce{TrackerUrl: address}).Do()
		if !errors.Is(err, companionguard.ErrDenied) {
			t.Fatalf("UDP tracker %s: %v", address, err)
		}
	}
}

func TestCompanionUDPRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (Announce{TrackerUrl: "udp://tracker.example:6969/announce", Context: ctx}).Do()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled tracker: %v", err)
	}
	_, err = (Announce{TrackerUrl: "udp://tracker.example:6969/announce", UdpNetwork: "udp6"}).Do()
	if !errors.Is(err, companionguard.ErrDenied) {
		t.Fatalf("IPv6 tracker: %v", err)
	}
}

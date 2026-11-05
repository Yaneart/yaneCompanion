package companionguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestSpecialAddresses(t *testing.T) {
	for _, address := range []string{
		"0.1.2.3:80", "10.1.2.3:80", "100.64.1.1:80", "127.0.0.1:80",
		"169.254.169.254:80", "172.16.0.1:80", "192.0.0.9:80", "192.0.2.1:80",
		"192.31.196.1:80", "192.52.193.1:80", "192.175.48.1:80",
		"192.88.99.1:80", "192.168.1.1:80", "198.18.0.1:80", "198.51.100.1:80",
		"203.0.113.1:80", "224.0.0.1:80", "255.255.255.255:80", "[::1]:80",
		"[::ffff:127.0.0.1]:80", "[2001:4860:4860::8888]:443", "[fe80::1%eth0]:80",
		"localhost:80", "2130706433:80", "127.1:80", "1.1.1.1:0", "1.1.1.1",
	} {
		if Public(address) || !errors.Is(Control("tcp4", address, nil), ErrDenied) {
			t.Errorf("permitted %s", address)
		}
	}
	for _, address := range []string{"1.1.1.1:443", "8.8.8.8:80", "93.184.216.34:6969"} {
		if !Public(address) {
			t.Errorf("rejected public %s", address)
		}
	}
	if Control("udp4", "1.1.1.1:80", nil) == nil || Control("tcp6", "[::ffff:1.1.1.1]:80", nil) == nil {
		t.Fatal("unsupported transport permitted")
	}
}

func TestLocalSocketsNeverConnect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("private service received a request")
	}))
	defer server.Close()
	for _, address := range []string{server.Listener.Addr().String(), "localhost:" + strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port)} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		conn, err := Dialer().DialContext(ctx, "tcp", address)
		cancel()
		if conn != nil {
			conn.Close()
		}
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("connect %s: %v", address, err)
		}
	}
}

func TestHTTPRedirectCannotReachPrivateService(t *testing.T) {
	private := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("redirect reached private service")
	}))
	defer private.Close()
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, private.URL, http.StatusFound)
	}))
	defer public.Close()
	transport := &http.Transport{Proxy: nil, DialContext: Dialer().DialContext}
	defer transport.CloseIdleConnections()
	// Simulate a reachable public first hop while preserving the actual policy
	// for the new connection created by the redirect.
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == "public.example:80" {
			return (&net.Dialer{}).DialContext(ctx, network, public.Listener.Addr().String())
		}
		return Dialer().DialContext(ctx, network, address)
	}
	response, err := (&http.Client{Transport: transport, Timeout: time.Second}).Get("http://public.example/start")
	if response != nil {
		response.Body.Close()
	}
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("redirect error: %v", err)
	}
}

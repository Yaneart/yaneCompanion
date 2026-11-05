package torrent

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/anacrolix/torrent/companionguard"
)

func TestCompanionPeerEgress(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	peer, err := listenTcp("tcp4", "127.0.0.1:0", "")
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	connection, err := peer.dial(ctx, listener.Addr().String())
	if connection != nil {
		connection.Close()
	}
	if !errors.Is(err, companionguard.ErrDenied) {
		t.Fatalf("peer dial reached local service: %v", err)
	}
}

func TestCompanionDisablesDiscoveryAndProxy(t *testing.T) {
	config := NewDefaultClientConfig()
	config.ListenHost = func(string) string { return "127.0.0.1" }
	config.ListenPort = 0
	config.NoDHT = false
	config.DisableUTP = false
	config.DisablePEX = false
	config.DisableIPv6 = false
	config.NoDefaultPortForwarding = false
	config.LocalServiceDiscovery = &LocalServiceDiscoveryConfig{}
	config.ProxyURL = "socks5://127.0.0.1:1"
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if !config.NoDHT || !config.DisableUTP || !config.DisablePEX || !config.DisableIPv6 || !config.NoDefaultPortForwarding || config.LocalServiceDiscovery != nil || config.ProxyURL != "" || config.HTTPProxy != nil {
		t.Fatal("hostile engine settings reenabled unguarded transport")
	}
	for _, address := range client.ListenAddrs() {
		if address.Network() != "tcp" {
			t.Fatalf("unguarded listener: %s", address.Network())
		}
	}
}

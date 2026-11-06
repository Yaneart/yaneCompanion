// Package companionguard is copied into the pinned torrent dependency at build
// time. The same policy protects peer sockets, HTTP trackers and metadata HTTP.
package companionguard

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

var ErrDenied = errors.New("COMPANION_EGRESS_DENIED")

// Only public IPv4 destinations are allowed. Deny special-purpose space,
// including globally reachable exceptions, rather than making private-network
// access depend on tracker URLs, DNS answers, redirects or settings files.
var reserved = [...]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"), netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/3"),
}

func Public(address string) bool {
	a, err := netip.ParseAddrPort(address)
	if err != nil || a.Port() == 0 || !a.Addr().Is4() {
		return false
	}
	for _, prefix := range reserved {
		if prefix.Contains(a.Addr()) {
			return false
		}
	}
	return true
}

// Control runs after DNS resolution and immediately before connect, for every
// attempted address. The kernel receives exactly the address checked here.
func Control(network, address string, _ syscall.RawConn) error {
	if network != "tcp4" && network != "tcp" {
		return ErrDenied
	}
	return destination(address)
}

func destination(address string) error {
	if !Public(address) {
		return ErrDenied
	}
	interfaces, err := net.InterfaceAddrs()
	if err != nil {
		return ErrDenied
	}
	a, _ := netip.ParseAddrPort(address)
	for _, local := range interfaces {
		prefix, err := netip.ParsePrefix(local.String())
		if err == nil && prefix.Contains(a.Addr()) {
			return ErrDenied
		}
	}
	return nil
}

func Dialer() *net.Dialer { return &net.Dialer{Timeout: 15 * time.Second, Control: Control} }

// Connected tracker sockets check the resolved destination before the first
// datagram and accept replies only from that endpoint. Peer UDP stays disabled.
func TrackerDialer() *net.Dialer {
	return &net.Dialer{Timeout: 15 * time.Second, Control: trackerControl}
}

func trackerControl(network, address string, _ syscall.RawConn) error {
	if network != "udp4" {
		return ErrDenied
	}
	return destination(address)
}

// Install replaces environment proxy discovery as well as the default dialer.
// Redirects are processed normally, but each new socket rechecks its address.
func Install() {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = Dialer().DialContext
	transport.DialTLSContext = nil
	transport.ResponseHeaderTimeout = 15 * time.Second
	http.DefaultTransport = transport
}

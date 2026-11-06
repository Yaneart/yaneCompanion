// Isolated integration fixture only; built against the locked engine module.
// Never installed in the helper bundle or reachable through website commands.
package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

type seededStorage []byte

func (s seededStorage) OpenTorrent(*metainfo.Info, metainfo.Hash) (storage.TorrentImpl, error) {
	return s, nil
}
func (s seededStorage) Close() error { return nil }
func (s seededStorage) Piece(p metainfo.Piece) storage.PieceImpl {
	return seedPiece{bytes.NewReader(s[p.Offset() : p.Offset()+p.Length()])}
}

type seedPiece struct{ *bytes.Reader }

func (p seedPiece) WriteAt([]byte, int64) (int, error) { return 0, io.ErrClosedPipe }
func (p seedPiece) MarkComplete() error                { return nil }
func (p seedPiece) MarkNotComplete() error             { return nil }
func (p seedPiece) Completion() storage.Completion {
	return storage.Completion{Complete: true, Ok: true}
}

func main() {
	root := "/tmp/seed-data"
	if err := os.MkdirAll(root, 0700); err != nil {
		log.Fatal(err)
	}
	data := bytes.Repeat([]byte("lc005 real piece data\n"), 200000)[:2<<20]
	if err := os.WriteFile(filepath.Join(root, "fixture.mkv"), data, 0600); err != nil {
		log.Fatal(err)
	}
	info := metainfo.Info{Name: "fixture.mkv", Length: int64(len(data)), PieceLength: 16384}
	for offset := 0; offset < len(data); offset += 16384 {
		sum := sha1.Sum(data[offset:min(offset+16384, len(data))])
		info.Pieces = append(info.Pieces, sum[:]...)
	}
	raw, err := bencode.Marshal(info)
	if err != nil {
		log.Fatal(err)
	}
	mi := &metainfo.MetaInfo{InfoBytes: raw, Announce: "http://seed.fixture:18080/announce"}
	if err := os.MkdirAll("/fixtures", 0700); err != nil {
		log.Fatal(err)
	}
	config := torrent.NewDefaultClientConfig()
	config.DataDir = root
	config.DefaultStorage = seededStorage(data)
	config.SetListenAddr("11.247.0.2:16881")
	config.Seed = true
	config.NoUpload = false
	config.NoDHT = true
	config.DisableUTP = true
	config.DisableIPv6 = true
	config.NoDefaultPortForwarding = true
	client, err := torrent.NewClient(config)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	seeded, err := client.AddTorrent(mi)
	if err != nil {
		log.Fatal(err)
	}
	seeded.VerifyData()
	deadline := time.Now().Add(10 * time.Second)
	for seeded.BytesCompleted() != int64(len(data)) {
		if time.Now().After(deadline) {
			log.Fatal("seeder verification deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	peers := string([]byte{11, 247, 0, 2, byte(16881 >> 8), byte(16881 & 255)})
	http.HandleFunc("/announce", func(w http.ResponseWriter, r *http.Request) {
		response, _ := bencode.Marshal(map[string]any{"interval": 60, "complete": 1, "incomplete": 0, "peers": peers})
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(response)
	})
	listener, err := net.Listen("tcp4", "11.247.0.2:18080")
	if err != nil {
		log.Fatal(err)
	}
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("11.247.0.2"), Port: 18081})
	if err != nil {
		log.Fatal(err)
	}
	defer udp.Close()
	go serveUDP(udp, []byte(peers))
	fixture, err := os.Create("/fixtures/fixture.torrent")
	if err != nil {
		log.Fatal(err)
	}
	if err := mi.Write(fixture); err != nil {
		log.Fatal(err)
	}
	fixture.Close()
	fmt.Println("Isolated seeder ready")
	log.Fatal(http.Serve(listener, nil))
}

// Minimal BEP 15 fixture: connect and announce, with one verified TCP seeder.
func serveUDP(socket *net.UDPConn, peers []byte) {
	var packet [2048]byte
	for {
		n, address, err := socket.ReadFromUDP(packet[:])
		if err != nil {
			return
		}
		if n < 16 || binary.BigEndian.Uint64(packet[:8]) != 0x41727101980 {
			continue
		}
		action := binary.BigEndian.Uint32(packet[8:12])
		var response []byte
		switch {
		case action == 0 && n == 16:
			response = make([]byte, 16)
			binary.BigEndian.PutUint64(response[8:], 0x41727101980)
		case action == 1 && n >= 98:
			response = make([]byte, 20+len(peers))
			binary.BigEndian.PutUint32(response, 1)
			binary.BigEndian.PutUint32(response[8:], 60)
			binary.BigEndian.PutUint32(response[16:], 1)
			copy(response[20:], peers)
		default:
			continue
		}
		copy(response[4:8], packet[12:16])
		_, _ = socket.WriteToUDP(response, address)
	}
}

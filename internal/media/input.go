package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"yanecompanion/internal/engine"
)

type RangeBackend interface {
	OpenRange(context.Context, string, engine.File, int64, int64) (io.ReadCloser, error)
}

func randomRef() string {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data[:])
}

// Only this unguessable, loopback input is visible to a worker. Small exact
// upstream ranges let a paused viewer release engine sockets without expiring
// FFmpeg's input. No redirects, secondary inputs or filesystem protocols.
type inputBridge struct {
	url         string
	server      *http.Server
	bytes       atomic.Int64
	exceeded    atomic.Bool
	unavailable atomic.Bool
}

type inputBudget struct {
	bytes atomic.Int64
	limit int64
}

var inputOptions = []string{"-protocol_whitelist", "http,tcp", "-format_whitelist", "mov,matroska,webm,avi,mpegts", "-max_streams", "64", "-max_pixels", "8847360", "-threads", "2", "-probesize", "8388608", "-analyzeduration", "8000000", "-max_alloc", "67108864"}

func bridge(ctx context.Context, backend RangeBackend, hash string, file engine.File, limit int64, shared ...*inputBudget) (*inputBridge, error) {
	budget := &inputBudget{limit: limit}
	if len(shared) > 0 {
		budget = shared[0]
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("MEDIA_UNAVAILABLE")
	}
	b := &inputBridge{}
	path := "/" + randomRef()
	host := listener.Addr().String()
	b.url = "http://" + host + path
	slots := make(chan struct{}, 2)
	b.server = &http.Server{ReadHeaderTimeout: 2 * time.Second, MaxHeaderBytes: 4096, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host || r.URL.Path != path || r.URL.RawQuery != "" || r.URL.RawPath != "" || r.Header.Get("Origin") != "" || r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(403)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.WriteHeader(429)
			return
		}
		start, end := int64(0), file.Length-1
		status := 200
		if value := r.Header.Get("Range"); value != "" {
			parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
			var parseErr error
			if !strings.HasPrefix(value, "bytes=") || len(parts) != 2 {
				w.WriteHeader(416)
				return
			}
			start, parseErr = strconv.ParseInt(parts[0], 10, 64)
			if parseErr != nil {
				w.WriteHeader(416)
				return
			}
			if parts[1] != "" {
				end, parseErr = strconv.ParseInt(parts[1], 10, 64)
			}
			if parseErr != nil || start < 0 || end < start || start >= file.Length {
				w.WriteHeader(416)
				return
			}
			end = min(end, file.Length-1)
			status = 206
			w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(file.Length, 10))
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(status)
		if r.Method == "HEAD" {
			return
		}
		requestCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		stop := context.AfterFunc(r.Context(), cancel)
		defer stop()
		for start <= end {
			size := min(int64(256<<10), end-start+1)
			if budget.limit > 0 && budget.bytes.Add(size) > budget.limit {
				b.exceeded.Store(true)
				return
			}
			readCtx, finish := context.WithTimeout(requestCtx, 120*time.Second)
			body, err := backend.OpenRange(readCtx, hash, file, start, start+size-1)
			if err != nil {
				if requestCtx.Err() == nil {
					b.unavailable.Store(true)
				}
				finish()
				return
			}
			n, err := io.CopyN(w, body, size)
			body.Close()
			finish()
			b.bytes.Add(n)
			if err != nil {
				if requestCtx.Err() == nil && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
					b.unavailable.Store(true)
				}
				return
			}
			start += n
		}
	})}
	go func() { _ = b.server.Serve(listener) }()
	return b, nil
}

func (b *inputBridge) Close() { _ = b.server.Close() }

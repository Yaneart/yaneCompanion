package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"

	"yanecompanion/internal/locator"
)

var ErrUnavailable = errors.New("ENGINE_UNAVAILABLE")

type File struct {
	Index  int    `json:"id"`
	Path   string `json:"path"`
	Length int64  `json:"length"`
}

func (e *Engine) call(ctx context.Context, method, route, contentType string, body io.Reader, limit int64) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(ctx, method, e.address+route, body)
	if err != nil {
		return nil, 0, ErrUnavailable
	}
	request.Header.Set("Content-Type", contentType)
	request.SetBasicAuth("companion", e.password)
	// The startup client's short timeout must not limit peer metadata/stream work.
	client := *e.client
	client.Timeout = 0
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, ErrUnavailable
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, response.StatusCode, ErrUnavailable
	}
	return data, response.StatusCode, nil
}

func (e *Engine) post(ctx context.Context, payload any) ([]byte, int, error) {
	body, _ := json.Marshal(payload)
	return e.call(ctx, "POST", "/torrents", "application/json", bytes.NewReader(body), 1<<20)
}

func (e *Engine) admit(ctx context.Context, input locator.Input) error {
	var data []byte
	var status int
	var err error
	if input.Magnet() != "" {
		data, status, err = e.post(ctx, map[string]any{"action": "add", "link": input.Magnet(), "save_to_db": false})
	} else {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, createErr := form.CreateFormFile("file", "input.torrent")
		if createErr != nil {
			return ErrUnavailable
		}
		_, _ = file.Write(input.Metainfo())
		_ = form.Close()
		data, status, err = e.call(ctx, "POST", "/torrent/upload", form.FormDataContentType(), &body, 1<<20)
	}
	if err != nil || status != 200 {
		return ErrUnavailable
	}
	var result struct {
		Hash string `json:"hash"`
	}
	if len(data) > 0 && data[0] == '[' {
		var list []struct {
			Hash string `json:"hash"`
		}
		if json.Unmarshal(data, &list) != nil || len(list) != 1 {
			return ErrUnavailable
		}
		result.Hash = list[0].Hash
	} else if json.Unmarshal(data, &result) != nil {
		return ErrUnavailable
	}
	if result.Hash != input.Hash() {
		return ErrUnavailable
	}
	return nil
}

func (e *Engine) Files(ctx context.Context, hash string) ([]File, error) {
	data, status, err := e.post(ctx, map[string]string{"action": "get", "hash": hash})
	if err != nil || status != 200 {
		return nil, ErrUnavailable
	}
	var result struct {
		Hash  string `json:"hash"`
		Files []File `json:"file_stats"`
	}
	if json.Unmarshal(data, &result) != nil || result.Hash != hash || len(result.Files) > 500 {
		return nil, ErrUnavailable
	}
	seen := map[int]bool{}
	total := int64(0)
	for _, file := range result.Files {
		if file.Index < 1 || file.Path == "" || file.Length < 0 || file.Length > locator.MaxSize || seen[file.Index] {
			return nil, locator.ErrUnsafe
		}
		seen[file.Index] = true
		total += file.Length
	}
	if total > locator.MaxSize {
		return nil, locator.ErrUnsafe
	}
	return result.Files, nil
}

// LC-005 proves local piece delivery with a bounded prefix. Rolling preparation
// belongs to LC-006; never download an entire film or expose this raw URL.
func (e *Engine) Download(ctx context.Context, hash string, file File) (int64, error) {
	size := min(file.Length, int64(1<<20))
	if size < 1 {
		return 0, locator.ErrUnsafe
	}
	query := url.Values{"link": {hash}, "index": {strconv.Itoa(file.Index)}, "play": {""}}
	request, _ := http.NewRequestWithContext(ctx, "GET", e.address+"/stream?"+query.Encode(), nil)
	request.SetBasicAuth("companion", e.password)
	request.Header.Set("Range", "bytes=0-"+strconv.FormatInt(size-1, 10))
	client := *e.client
	client.Timeout = 0
	response, err := client.Do(request)
	if err != nil {
		return 0, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 206 || response.ContentLength != size || response.Header.Get("Content-Range") != "bytes 0-"+strconv.FormatInt(size-1, 10)+"/"+strconv.FormatInt(file.Length, 10) {
		return 0, ErrUnavailable
	}
	n, err := io.CopyN(io.Discard, response.Body, size)
	if err != nil {
		return n, ErrUnavailable
	}
	return n, nil
}

func (e *Engine) Drop(ctx context.Context, hash string) error {
	_, status, err := e.post(ctx, map[string]string{"action": "drop", "hash": hash})
	if err != nil || status != 200 && status != 404 {
		return ErrUnavailable
	}
	return nil
}

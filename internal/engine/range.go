package engine

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// OpenRange is private to the media worker bridge. Neither its URL nor the
// engine credential is returned to the browser or passed to FFmpeg.
func (e *Engine) OpenRange(ctx context.Context, hash string, file File, start, end int64) (io.ReadCloser, error) {
	if start < 0 || end < start || end >= file.Length {
		return nil, ErrUnavailable
	}
	query := url.Values{"link": {hash}, "index": {strconv.Itoa(file.Index)}, "play": {""}}
	req, _ := http.NewRequestWithContext(ctx, "GET", e.address+"/stream?"+query.Encode(), nil)
	req.SetBasicAuth("companion", e.password)
	req.Header.Set("Range", "bytes="+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10))
	client := *e.client
	client.Timeout = 0
	res, err := client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	want := "bytes " + strconv.FormatInt(start, 10) + "-" + strconv.FormatInt(end, 10) + "/" + strconv.FormatInt(file.Length, 10)
	if res.StatusCode != 206 || res.ContentLength != end-start+1 || res.Header.Get("Content-Range") != want {
		res.Body.Close()
		return nil, ErrUnavailable
	}
	return res.Body, nil
}

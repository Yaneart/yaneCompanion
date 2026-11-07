package api

import (
	"net/http"
	"regexp"
	"strings"
	"yanecompanion/internal/sessions"
)

var mediaRoute = regexp.MustCompile(`^/v1/media/[0-9a-f]{64}/(master\.m3u8|media\.m3u8|init\.mp4|segment-[0-9]{6,9}\.m4s|text-[0-9a-f]{64}\.(vtt|ass)|font-[0-9a-f]{64}\.bin)$`)

func (h Handler) mediaAPI(w http.ResponseWriter, r *http.Request, origin string) {
	if h.Sessions == nil || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
		fail(w, 404, "NOT_FOUND")
		return
	}
	if r.Header.Get("Range") != "" {
		fail(w, 416, "RANGE_NOT_SATISFIABLE")
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	playback, err := h.Sessions.ReadMedia(origin, parts[3])
	if err != nil {
		status, code := sessions.Code(err)
		fail(w, status, code)
		return
	}
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	playback.Serve(w, r, parts[4])
}

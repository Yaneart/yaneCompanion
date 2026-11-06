`tiny-h264.mp4` is the deterministic 24-second 128x72 H.264 fixture from
yanePlayer `tests/fixtures/event-hls.json`: init followed by its twelve fragments.
It has no audio. Tests use the verified bundled FFmpeg to remux/loop it; no
download, system FFmpeg, encoder library or network is required.

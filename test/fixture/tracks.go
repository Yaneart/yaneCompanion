// Package fixture builds small, deterministic media for focused track checks.
package fixture

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Tracks contains H264, two distinct PCM tones, SRT, authored ASS, one font,
// plus an exact-name external sidecar and an unrelated episode subtitle.
func Tracks(t testing.TB, ffmpeg, tiny string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	wave := func(hz float64) string {
		const rate = 48000
		data := make([]byte, 44+rate*2)
		copy(data, "RIFF")
		binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
		copy(data[8:], "WAVEfmt ")
		binary.LittleEndian.PutUint32(data[16:], 16)
		binary.LittleEndian.PutUint16(data[20:], 1)
		binary.LittleEndian.PutUint16(data[22:], 1)
		binary.LittleEndian.PutUint32(data[24:], rate)
		binary.LittleEndian.PutUint32(data[28:], rate*2)
		binary.LittleEndian.PutUint16(data[32:], 2)
		binary.LittleEndian.PutUint16(data[34:], 16)
		copy(data[36:], "data")
		binary.LittleEndian.PutUint32(data[40:], rate*2)
		for i := 0; i < rate; i++ {
			binary.LittleEndian.PutUint16(data[44+i*2:], uint16(int16(math.Sin(float64(i)*2*math.Pi*hz/rate)*16000)))
		}
		return write(fmt.Sprintf("%.0f.wav", hz), data)
	}
	var srt, events string
	for i := 0; i < 96; i++ {
		seconds := i * 2
		clock := fmt.Sprintf("00:%02d:%02d", seconds/60, seconds%60)
		end := fmt.Sprintf("00:%02d:%02d", (seconds+2)/60, (seconds+2)%60)
		srt += fmt.Sprintf("%d\n%s,000 --> %s,000\nTEXT %03d\n\n", i+1, clock, end, i)
		if i == 10 {
			srt += "1000\n00:00:20,000 --> 00:02:20,000\nLONG DELAY\n\n"
		}
		events += fmt.Sprintf("Dialogue: 0,%s.00,%s.00,Default,,0,0,0,,ASS %03d\n", clock[1:], end[1:], i)
	}
	sub := write("embedded.srt", []byte(srt))
	ass := write("embedded.ass", []byte("[Script Info]\nScriptType: v4.00+\nPlayResX: 320\nPlayResY: 180\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Default,Ink Free,18,&H00FFFFFF,&H000000FF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,1,0,2,10,10,12,1\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n"+events))
	font := make([]byte, 32)
	binary.BigEndian.PutUint32(font, 0x00010000)
	binary.BigEndian.PutUint16(font[4:], 1)
	copy(font[12:], "name")
	binary.BigEndian.PutUint32(font[20:], 28)
	binary.BigEndian.PutUint32(font[24:], 4)
	if fontPath := os.Getenv("YANE_TEST_FONT"); fontPath != "" {
		var err error
		font, err = os.ReadFile(fontPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	fontPath := write("fixture.ttf", font)
	output := filepath.Join(dir, "episode.mkv")
	args := []string{"-v", "error", "-nostdin", "-stream_loop", "7", "-i", tiny, "-stream_loop", "-1", "-i", wave(440), "-stream_loop", "-1", "-i", wave(880), "-i", sub, "-i", ass, "-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "3:s", "-map", "4:s", "-c", "copy", "-metadata:s:a:0", "title=Tone 440", "-metadata:s:a:0", "language=eng", "-metadata:s:a:1", "title=Tone 880", "-metadata:s:a:1", "language=rus", "-metadata:s:s:0", "language=eng", "-disposition:s:0", "forced+hearing_impaired", "-attach", fontPath, "-metadata:s:t:0", "mimetype=application/x-truetype-font", "-t", "192", output}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if data, err := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("tracks fixture: %v %s", err, data)
	}
	write("episode.rus.sdh.srt", []byte(srt))
	assData, err := os.ReadFile(ass)
	if err != nil {
		t.Fatal(err)
	}
	write("episode.rus.ass", assData)
	write("other.rus.srt", []byte(srt))
	return output
}

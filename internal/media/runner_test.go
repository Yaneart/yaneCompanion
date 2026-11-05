package media

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yanecompanion/release"
)

func waveFixture(t *testing.T) string {
	t.Helper()
	const sampleRate = 48000
	const samples = sampleRate * 2
	data := make([]byte, 44+samples*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], sampleRate)
	binary.LittleEndian.PutUint32(data[28:], sampleRate*2)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], samples*2)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(int16(math.Sin(float64(i)*2*math.Pi*440/sampleRate)*16000)))
	}
	path := filepath.Join(t.TempDir(), "audio input.wav")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRealMediaWorkers(t *testing.T) {
	executable := os.Getenv("YANE_TEST_EXECUTABLE")
	if executable == "" {
		t.Skip("requires a built bundle")
	}
	bundle, err := release.Verify(filepath.Dir(executable))
	if err != nil {
		t.Fatal(err)
	}
	runner := New(bundle.FFmpeg, bundle.FFprobe)
	input := waveFixture(t)
	output := filepath.Join(filepath.Dir(input), "encoded.m4a")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := runner.Prepare(ctx, []string{"-nostdin", "-v", "error", "-i", input, "-c:a", "aac", "-b:a", "128k", output}); err != nil {
		t.Fatal(err)
	}
	data, err := runner.Probe(ctx, []string{"-v", "error", "-show_streams", "-show_format", "-of", "json", output})
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
		}
		Format struct {
			Duration string `json:"duration"`
		}
	}
	if err := json.Unmarshal(data, &probe); err != nil || len(probe.Streams) != 1 || probe.Streams[0].CodecName != "aac" || probe.Format.Duration == "" {
		t.Fatalf("invalid actual probe: %v %s", err, data)
	}
	decoded, err := runner.Prepare(ctx, []string{"-nostdin", "-v", "error", "-i", output, "-f", "s16le", "-acodec", "pcm_s16le", "pipe:1"})
	if err != nil || len(decoded) < 48000*2 {
		t.Fatalf("decode failed: %v bytes=%d", err, len(decoded))
	}
	// A running worker must honor cancellation and release the lane for the next job.
	short, abort := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer abort()
	_, err = runner.Prepare(short, []string{"-nostdin", "-v", "error", "-stream_loop", "-1", "-re", "-i", input, "-f", "null", "-"})
	if err == nil || short.Err() == nil {
		t.Fatalf("worker ignored cancellation: %v", err)
	}
	_, err = runner.Prepare(ctx, []string{"-nostdin", "-v", "error", "-stream_loop", "-1", "-i", input, "-f", "s16le", "pipe:1"})
	if err == nil || err.Error() != "OUTPUT_LIMIT" {
		t.Fatalf("unbounded worker output: %v", err)
	}
	if _, err := runner.Probe(ctx, []string{"-version"}); err != nil {
		t.Fatalf("worker lane leaked: %v", err)
	}
}

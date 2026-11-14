package diagnostics

import (
	"errors"
	"strings"
	"testing"
)

func TestLC011ReportNeverIncludesUntrustedInput(t *testing.T) {
	secret := `magnet:?xt=urn:btih:0123456789012345678901234567890123456789 C:\Users\private\secret.mkv password=a token=abcdef`
	report := Report(errors.New(secret), errors.New(secret), secret, errors.New(secret), secret)
	for _, value := range strings.Fields(secret) {
		if strings.Contains(report, value) {
			t.Fatalf("private value in report: %s", value)
		}
	}
	if !strings.Contains(report, "OPERATION_FAILED") || !strings.Contains(report, "Playback: idle") {
		t.Fatal(report)
	}
	known := Report(errors.New("BUNDLE_HASH_MISMATCH"), nil, "failed", errors.New("NO_PEERS"), "UPDATE_SIGNATURE_INVALID")
	for _, code := range []string{"BUNDLE_HASH_MISMATCH", "NO_PEERS", "UPDATE_SIGNATURE_INVALID"} {
		if !strings.Contains(known, code) {
			t.Fatal(known)
		}
	}
}

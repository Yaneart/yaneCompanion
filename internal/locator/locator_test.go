package locator

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func bencode(v any) []byte {
	switch x := v.(type) {
	case string:
		return []byte(fmt.Sprintf("%d:%s", len(x), x))
	case int:
		return []byte(fmt.Sprintf("i%de", x))
	case []any:
		b := []byte{'l'}
		for _, i := range x {
			b = append(b, bencode(i)...)
		}
		return append(b, 'e')
	case map[string]any:
		keys := []string{}
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b := []byte{'d'}
		for _, k := range keys {
			b = append(b, bencode(k)...)
			b = append(b, bencode(x[k])...)
		}
		return append(b, 'e')
	}
	panic("fixture type")
}
func infoFixture() map[string]any {
	return map[string]any{"name": "film.mkv", "length": 16384, "piece length": 16384, "pieces": strings.Repeat("x", 20)}
}
func metainfoFixture(info map[string]any) []byte {
	return bencode(map[string]any{"announce": "https://tracker.example/announce?passkey=local-secret", "info": info})
}
func TestValidatedInput(t *testing.T) {
	raw := metainfoFixture(infoFixture())
	input, err := Validate(Locator{Kind: "metainfo", Base64: base64.StdEncoding.EncodeToString(raw)})
	if err != nil || !input.Valid() || len(input.Hash()) != 40 || !bytes.Equal(input.Metainfo(), raw) {
		t.Fatal("valid private metainfo rejected", err)
	}
	for _, hash := range []string{strings.Repeat("a", 40), strings.Repeat("A", 32)} {
		input, err := Validate(Locator{Kind: "magnet", Value: "magnet:?xt=urn:btih:" + hash + "&dn=Display&tr=https%3A%2F%2Ftracker.example%2Fannounce"})
		if err != nil || !input.Valid() || strings.Contains(input.Magnet(), "dn=") {
			t.Fatal("valid magnet rejected", err)
		}
	}
}
func TestUnsafeLocators(t *testing.T) {
	valid := "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)
	for _, raw := range []string{
		"https://tracker.example/file.torrent", "magnet:?xt=" + strings.Repeat("a", 40), valid + "&xt=urn:btih:" + strings.Repeat("b", 40),
		valid + "&xs=http://127.0.0.1/private", valid + "&x.pe=127.0.0.1:80", valid + "&tr=http://127.0.0.1/announce", valid + "&tr=http://localhost/announce", valid + "&tr=http://u:p@tracker.example/announce", valid + "&tr=http://tracker.local/announce", valid + "&xl=999999999999999999999", valid + strings.Repeat("x", 4096),
	} {
		if _, err := Validate(Locator{Kind: "magnet", Value: raw}); err == nil {
			t.Fatal("unsafe locator accepted")
		}
	}
	for _, raw := range []string{"", "%%%%", base64.StdEncoding.EncodeToString([]byte("de"))} {
		if _, err := Validate(Locator{Kind: "metainfo", Base64: raw}); err == nil {
			t.Fatal("unsafe metainfo accepted")
		}
	}
	if _, err := Validate(Locator{Kind: "magnet", Value: valid, Base64: "data"}); err == nil {
		t.Fatal("mixed locator accepted")
	}
}
func TestUnsafeMetadataBeforeStorage(t *testing.T) {
	for _, name := range []string{"../film.mkv", "C:film.mkv", "film\\evil.mkv", "CON.mkv", "film.mkv.", "film\x00.mkv", "..", "NUL", "LPT1.txt"} {
		info := infoFixture()
		info["name"] = name
		if ValidateInfo(bencode(info)) == nil {
			t.Fatalf("unsafe name accepted: %q", name)
		}
	}
	for _, key := range []string{"meta version", "url-list", "symlink path", "attr"} {
		info := infoFixture()
		info[key] = 1
		if ValidateInfo(bencode(info)) == nil {
			t.Fatal("unsafe info key accepted", key)
		}
	}
	info := infoFixture()
	info["pieces"] = "wrong"
	if ValidateInfo(bencode(info)) == nil {
		t.Fatal("bad piece geometry accepted")
	}
	info = infoFixture()
	delete(info, "length")
	info["files"] = []any{map[string]any{"length": 8192, "path": []any{"episode.mkv"}}, map[string]any{"length": 8192, "path": []any{"EPISODE.mkv"}}}
	if ValidateInfo(bencode(info)) == nil {
		t.Fatal("case collision accepted")
	}
	info["files"] = []any{map[string]any{"length": 8192, "path": []any{"dir"}}, map[string]any{"length": 8192, "path": []any{"dir", "episode.mkv"}}}
	if ValidateInfo(bencode(info)) == nil {
		t.Fatal("file/directory collision accepted")
	}
	info["files"] = []any{map[string]any{"length": 16384, "path": []any{"episode.mkv"}, "attr": "l"}}
	if ValidateInfo(bencode(info)) == nil {
		t.Fatal("symlink accepted")
	}
	root := map[string]any{"info": infoFixture(), "url-list": "https://tracker.example/video"}
	raw := bencode(root)
	if _, err := Validate(Locator{Kind: "metainfo", Base64: base64.StdEncoding.EncodeToString(raw)}); err == nil {
		t.Fatal("webseed accepted")
	}
	for _, data := range [][]byte{[]byte("d1:ai01ee"), []byte("d1:ai1e1:ai2ee"), []byte("d1:bi1e1:ai2ee"), []byte("deextra"), bytes.Repeat([]byte{'l'}, 100), bytes.Repeat([]byte{'x'}, MaxBytes+1)} {
		if _, err := decode(data); err == nil {
			t.Fatal("malformed bencode accepted")
		}
	}
}

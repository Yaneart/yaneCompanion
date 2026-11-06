// Package locator validates the complete v1 torrent input before admission.
// Its standard-library-only code is also compiled into the private engine so
// peer-supplied magnet metadata passes the same checks before storage is opened.
package locator

import (
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxBytes = 2 << 20
const MaxSize int64 = 200 << 30

var ErrUnsafe = errors.New("UNSAFE_LOCATOR")

type Locator struct {
	Kind   string `json:"kind"`
	Value  string `json:"value,omitempty"`
	Base64 string `json:"base64,omitempty"`
}

type Input struct {
	hash, magnet string
	metainfo     []byte
}

func (i Input) Hash() string     { return i.hash }
func (i Input) Magnet() string   { return i.magnet }
func (i Input) Metainfo() []byte { return i.metainfo }
func (i Input) Valid() bool      { return len(i.hash) == 40 && (i.magnet != "" || len(i.metainfo) > 0) }

func Validate(l Locator) (Input, error) {
	if l.Kind == "magnet" && l.Base64 == "" {
		return magnet(l.Value)
	}
	if l.Kind != "metainfo" || l.Value != "" || len(l.Base64) > base64.StdEncoding.EncodedLen(MaxBytes) {
		return Input{}, ErrUnsafe
	}
	data, err := base64.StdEncoding.Strict().DecodeString(l.Base64)
	if err != nil || base64.StdEncoding.EncodeToString(data) != l.Base64 {
		return Input{}, ErrUnsafe
	}
	root, err := decode(data)
	if err != nil || root.dict == nil || !keys(root, "info", "announce", "announce-list", "creation date", "comment", "created by", "encoding", "publisher", "publisher-url", "publisher.utf-8", "publisher-url.utf-8") {
		return Input{}, ErrUnsafe
	}
	info, ok := root.dict["info"]
	if !ok {
		return Input{}, ErrUnsafe
	}
	if err := ValidateInfo(data[info.start:info.end]); err != nil {
		return Input{}, err
	}
	trackers := 0
	if announce, ok := root.dict["announce"]; ok {
		if !tracker(announce.text()) {
			return Input{}, ErrUnsafe
		}
		trackers++
	}
	if tiers, ok := root.dict["announce-list"]; ok {
		if tiers.list == nil || len(tiers.list) > 20 {
			return Input{}, ErrUnsafe
		}
		for _, tier := range tiers.list {
			if tier.list == nil || len(tier.list) == 0 {
				return Input{}, ErrUnsafe
			}
			for _, item := range tier.list {
				if !tracker(item.text()) {
					return Input{}, ErrUnsafe
				}
				trackers++
			}
		}
	}
	if trackers > 20 {
		return Input{}, ErrUnsafe
	}
	for key, item := range root.dict {
		if key == "info" || key == "announce-list" || key == "announce" {
			continue
		}
		if key == "creation date" {
			if item.number == nil || *item.number < 0 {
				return Input{}, ErrUnsafe
			}
		} else if item.bytes == nil || !utf8.Valid(item.bytes) || len(item.bytes) > 4096 {
			return Input{}, ErrUnsafe
		}
	}
	sum := sha1.Sum(data[info.start:info.end])
	return Input{hash: hex.EncodeToString(sum[:]), metainfo: append([]byte(nil), data...)}, nil
}

func magnet(raw string) (Input, error) {
	if len(raw) > 4096 || !strings.HasPrefix(raw, "magnet:?") {
		return Input{}, ErrUnsafe
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host != "" || u.Path != "" || u.Opaque != "" || u.Fragment != "" || u.User != nil {
		return Input{}, ErrUnsafe
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["xt"]) != 1 || len(q["tr"]) > 20 {
		return Input{}, ErrUnsafe
	}
	if !strings.HasPrefix(q.Get("xt"), "urn:btih:") {
		return Input{}, ErrUnsafe
	}
	hash := strings.TrimPrefix(q.Get("xt"), "urn:btih:")
	var b []byte
	if len(hash) == 40 {
		b, err = hex.DecodeString(hash)
	} else if len(hash) == 32 {
		b, err = base32.StdEncoding.DecodeString(strings.ToUpper(hash))
	} else {
		return Input{}, ErrUnsafe
	}
	if err != nil || len(b) != 20 {
		return Input{}, ErrUnsafe
	}
	count := 0
	for key, values := range q {
		count += len(values)
		if key != "xt" && key != "tr" && key != "dn" && key != "xl" {
			return Input{}, ErrUnsafe
		}
		for _, value := range values {
			if value == "" || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
				return Input{}, ErrUnsafe
			}
		}
	}
	if count > 32 || len(q["dn"]) > 1 || len(q["xl"]) > 1 {
		return Input{}, ErrUnsafe
	}
	if q.Has("xl") {
		n, err := strconv.ParseInt(q.Get("xl"), 10, 64)
		if err != nil || n < 1 || n > MaxSize {
			return Input{}, ErrUnsafe
		}
	}
	for _, raw := range q["tr"] {
		if !tracker(raw) {
			return Input{}, ErrUnsafe
		}
	}
	normalized := url.Values{"xt": {"urn:btih:" + hex.EncodeToString(b)}}
	for _, raw := range q["tr"] {
		normalized.Add("tr", raw)
	}
	return Input{hash: hex.EncodeToString(b), magnet: "magnet:?" + normalized.Encode()}, nil
}

func tracker(raw string) bool {
	if len(raw) == 0 || len(raw) > 2048 || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "udp") || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host != "" && net.ParseIP(host) == nil && host != "localhost" && !strings.HasSuffix(host, ".localhost") && !strings.HasSuffix(host, ".local") && !strings.ContainsAny(host, "\\% ")
}

// ValidateInfo also protects metadata received from peers through a magnet.
func ValidateInfo(data []byte) error {
	info, err := decode(data)
	if err != nil || info.dict == nil || !keys(info, "name", "name.utf-8", "piece length", "pieces", "length", "files", "private", "source") {
		return ErrUnsafe
	}
	name := info.dict["name"].text()
	if !part(name) {
		return ErrUnsafe
	}
	if value, ok := info.dict["name.utf-8"]; ok && (!part(value.text()) || value.text() != name) {
		return ErrUnsafe
	}
	if value, ok := info.dict["private"]; ok && (value.number == nil || (*value.number != 0 && *value.number != 1)) {
		return ErrUnsafe
	}
	if value, ok := info.dict["source"]; ok && (value.bytes == nil || !utf8.Valid(value.bytes) || len(value.bytes) > 512) {
		return ErrUnsafe
	}
	piece := info.dict["piece length"].number
	pieces := info.dict["pieces"].bytes
	if piece == nil || *piece < 16384 || *piece > 16<<20 || *piece&(*piece-1) != 0 || len(pieces) == 0 || len(pieces)%20 != 0 {
		return ErrUnsafe
	}
	total := int64(0)
	files, multi := info.dict["files"]
	length, single := info.dict["length"]
	if multi == single {
		return ErrUnsafe
	}
	if single {
		if length.number == nil || *length.number < 1 {
			return ErrUnsafe
		}
		total = *length.number
	} else {
		if files.list == nil || len(files.list) == 0 || len(files.list) > 500 {
			return ErrUnsafe
		}
		paths := map[string]bool{}
		for _, file := range files.list {
			if file.dict == nil || !keys(file, "length", "path", "path.utf-8") {
				return ErrUnsafe
			}
			n := file.dict["length"].number
			parts := file.dict["path"].list
			if n == nil || *n < 0 || *n > MaxSize || len(parts) == 0 || len(parts) > 32 {
				return ErrUnsafe
			}
			names := make([]string, len(parts))
			for i, p := range parts {
				names[i] = p.text()
				if !part(names[i]) {
					return ErrUnsafe
				}
			}
			if alt, ok := file.dict["path.utf-8"]; ok {
				if len(alt.list) != len(parts) {
					return ErrUnsafe
				}
				for i, p := range alt.list {
					if p.text() != names[i] {
						return ErrUnsafe
					}
				}
			}
			key := strings.ToLower(strings.Join(names, "/"))
			if len(key) > 4096 || paths[key] {
				return ErrUnsafe
			}
			paths[key] = true
			total += *n
			if total > MaxSize {
				return ErrUnsafe
			}
		}
		for key := range paths {
			for dir := path.Dir(key); dir != "."; dir = path.Dir(dir) {
				if paths[dir] {
					return ErrUnsafe
				}
			}
		}
	}
	if total <= 0 || total > MaxSize || int64(len(pieces)/20) != (total+*piece-1) / *piece {
		return ErrUnsafe
	}
	return nil
}

func part(s string) bool {
	if s == "" || !utf8.ValidString(s) || len([]rune(s)) > 255 || s == "." || s == ".." || strings.TrimRight(s, ". ") != s || strings.ContainsAny(s, "/\\:<>\"|?*") || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return false
	}
	stem := strings.ToUpper(strings.SplitN(s, ".", 2)[0])
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || stem == "CLOCK$" {
		return false
	}
	return !(len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '0' && stem[3] <= '9')
}

type node struct {
	start, end int
	bytes      []byte
	number     *int64
	list       []node
	dict       map[string]node
}

func (n node) text() string {
	if !utf8.Valid(n.bytes) {
		return ""
	}
	return string(n.bytes)
}
func keys(n node, allowed ...string) bool {
	for key := range n.dict {
		found := false
		for _, a := range allowed {
			if key == a {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func decode(data []byte) (node, error) {
	if len(data) == 0 || len(data) > MaxBytes {
		return node{}, ErrUnsafe
	}
	offset, nodes := 0, 0
	var parse func(int) (node, error)
	parse = func(depth int) (node, error) {
		n := node{start: offset}
		nodes++
		if offset >= len(data) || depth > 32 || nodes > 20000 {
			return n, ErrUnsafe
		}
		first := data[offset]
		offset++
		switch first {
		case 'i':
			end := offset
			for end < len(data) && data[end] != 'e' {
				end++
			}
			if end == len(data) {
				return n, ErrUnsafe
			}
			raw := string(data[offset:end])
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || strconv.FormatInt(value, 10) != raw {
				return n, ErrUnsafe
			}
			n.number = &value
			offset = end + 1
		case 'l', 'd':
			if first == 'l' {
				n.list = []node{}
			} else {
				n.dict = map[string]node{}
			}
			previous := ""
			for offset < len(data) && data[offset] != 'e' {
				key := ""
				if first == 'd' {
					k, err := parse(depth + 1)
					if err != nil || k.bytes == nil || !utf8.Valid(k.bytes) {
						return n, ErrUnsafe
					}
					key = string(k.bytes)
					if len(n.dict) > 0 && key <= previous {
						return n, ErrUnsafe
					}
					previous = key
				}
				item, err := parse(depth + 1)
				if err != nil {
					return n, err
				}
				if first == 'l' {
					n.list = append(n.list, item)
				} else {
					n.dict[key] = item
				}
			}
			if offset >= len(data) {
				return n, ErrUnsafe
			}
			offset++
		default:
			if first < '0' || first > '9' {
				return n, ErrUnsafe
			}
			start := offset - 1
			end := start
			for end < len(data) && data[end] != ':' && end-start <= 8 {
				end++
			}
			if end >= len(data) || data[end] != ':' {
				return n, ErrUnsafe
			}
			raw := string(data[start:end])
			size, err := strconv.Atoi(raw)
			if err != nil || size < 0 || strconv.Itoa(size) != raw || size > len(data)-end-1 {
				return n, ErrUnsafe
			}
			offset = end + 1
			n.bytes = data[offset : offset+size]
			offset += size
		}
		n.end = offset
		return n, nil
	}
	result, err := parse(0)
	if err != nil || offset != len(data) {
		return node{}, ErrUnsafe
	}
	return result, nil
}

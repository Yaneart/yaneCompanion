// patch-engine applies checked, deliberately small changes to locked upstream
// sources. It never edits the module cache or downloads an unpinned revision.
package main

import (
	"flag"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	source := flag.String("source", "", "extracted TorrServer server directory")
	dependency := flag.String("torrent", "", "verified tsynik/torrent v1.2.34 module directory")
	guard := flag.String("guard", "build/engine", "companion policy source directory")
	flag.Parse()
	if err := patch(*source, *dependency, *guard); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func patch(source, dependency, guard string) error {
	if source == "" || dependency == "" {
		return fmt.Errorf("source and torrent directories are required")
	}
	destination := filepath.Join(source, "companion-torrent")
	if err := os.CopyFS(destination, os.DirFS(dependency)); err != nil {
		return err
	}
	// Module-cache files can be read-only on Windows; write replacements to a
	// new file in this private build tree instead of changing original sources.
	edit := func(path, before, after string) error {
		path = filepath.Join(source, filepath.FromSlash(path))
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		value := strings.ReplaceAll(string(data), "\r\n", "\n")
		if strings.Count(value, before) != 1 {
			return fmt.Errorf("patch context changed: %s", path)
		}
		data = []byte(strings.Replace(value, before, after, 1))
		if strings.HasSuffix(path, ".go") {
			data, err = format.Source(data)
			if err != nil {
				return fmt.Errorf("format %s: %w", path, err)
			}
		}
		if err := os.Chmod(path, 0600); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0600)
	}
	changes := [][3]string{
		{"torr/stream.go", "reader := t.NewReader(file)", "reader := t.NewReader(file, companionRange(req.Header.Get(\"Range\"), file.Length())...)"},
		{"torr/stream.go", "\tdefer t.CloseReader(reader)", "\treader.SetRequestContext(req.Context())\n\tdefer t.CloseReader(reader)"},
		{"torr/torrent.go", "func (t *Torrent) NewReader(file *torrent.File) *torrstor.Reader {", "func (t *Torrent) NewReader(file *torrent.File, bounds ...int64) *torrstor.Reader {"},
		{"torr/torrent.go", "reader := t.cache.NewReader(file)", "reader := t.cache.NewReader(file, bounds...)"},
		{"torr/storage/torrstor/cache.go", "func (c *Cache) NewReader(file *torrent.File) *Reader {\n\treturn newReader(file, c)\n}", "func (c *Cache) NewReader(file *torrent.File, bounds ...int64) *Reader {\n\treturn newReader(file, c, bounds...)\n}"},
		{"torr/storage/torrstor/reader.go", "\tfile      *torrent.File", "\tfile      *torrent.File\n\t// Immutable finite range, initialized before this reader enters the cache.\n\tbounded bool\n\trangeStart, rangeEnd int64\n\treadContext func([]byte) (int,error)"},
		{"torr/storage/torrstor/reader.go", "func newReader(file *torrent.File, cache *Cache) *Reader {", "func newReader(file *torrent.File, cache *Cache, bounds ...int64) *Reader {"},
		{"torr/storage/torrstor/reader.go", "\tr.file = file", "\tr.file = file\n\tif len(bounds) == 2 && bounds[0] >= 0 && bounds[1] > bounds[0] && bounds[1] <= file.Length() {\n\t\tr.bounded = true\n\t\tr.rangeStart, r.rangeEnd = bounds[0], bounds[1]\n\t}"},
		{"torr/storage/torrstor/reader.go", "\t\tn, err = r.Reader.Read(p)", "\t\tn, err = r.readRequest(p)"},
		{"torr/storage/torrstor/reader.go", "func (r *Reader) SetReadahead(length int64) {", "func (r *Reader) SetReadahead(length int64) {\n\tif r.bounded { length = 0 }"},
		{"torr/storage/torrstor/reader.go", "func (r *Reader) getOffsetRange() (int64, int64) {", "func (r *Reader) getOffsetRange() (int64, int64) {\n\tif r.bounded { return r.rangeStart, r.rangeEnd-1 }"},
		{"web/api/stream.go", "\tdata := \"\"\n\n\tnotAuth :=", "\tnotAuth :="},
		{"web/api/stream.go", "\ttor := torr.GetTorrent(spec.InfoHash.HexString())\n\tif tor != nil {\n\t\ttitle = tor.Title\n\t\tposter = tor.Poster\n\t\tdata = tor.Data\n\t\tcategory = tor.Category\n\t}\n\tif tor == nil || tor.Stat == state.TorrentInDB {\n\t\ttor, err = torr.AddTorrent(spec, title, poster, data, category)\n\t\tif err != nil {\n\t\t\tc.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{\"error\": fmt.Errorf(\"error adding torrent: %v\", err).Error()})\n\t\t\treturn\n\t\t}\n\t}", "\t// Streaming cannot create a torrent, including after a cancelled request.\n\ttor := torr.GetTorrent(spec.InfoHash.HexString())\n\tif tor == nil || tor.Stat == state.TorrentInDB {\n\t\tc.AbortWithStatus(http.StatusNotFound)\n\t\treturn\n\t}"},
		{"go.mod", "github.com/anacrolix/torrent v1.59.1 => github.com/tsynik/torrent v1.2.34", "github.com/anacrolix/torrent v1.59.1 => ./companion-torrent"},
		{"cmd/main.go", "\t\"server\"", "\t\"server\"\n\t\"github.com/anacrolix/torrent/companionguard\""},
		{"cmd/main.go", "func main() {", "func main() {\n\tcompanionguard.Install()"},
		{"cmd/main.go", "\tdnsResolve()", "\t// Use the OS resolver; all resolved connection addresses are guarded."},
		{"web/auth/auth.go", "\tif !settings.HttpAuth {\n\t\treturn\n\t}\n\taccs := getAccounts()\n\tif accs == nil {\n\t\treturn\n\t}\n\tengine.Use(BasicAuth(accs))", "\t// This private build fails closed even without an accounts file.\n\tengine.Use(BasicAuth(getAccounts()))"},
		{"web/auth/auth.go", "\t\tif found {\n\t\t\tc.Set(gin.AuthUserKey, user)\n\t\t}", "\t\tif !found {\n\t\t\tc.Header(\"WWW-Authenticate\", \"Basic realm=Authorization Required\")\n\t\t\tc.AbortWithStatus(http.StatusUnauthorized)\n\t\t\treturn\n\t\t}\n\t\tc.Set(gin.AuthUserKey, user)"},
		{"web/server.go", "\troute := gin.New()\n\troute.Use", "\troute := gin.New()\n\troute.Use(func(c *gin.Context) {\n\t\tif len(c.Request.Header.Values(\"Origin\")) != 0 || c.Request.Host != \"127.0.0.1:\"+settings.Port {\n\t\t\tc.AbortWithStatus(http.StatusForbidden)\n\t\t}\n\t})\n\tauth.SetupAuth(route)\n\troute.Use"},
		{"web/server.go", "\tauth.SetupAuth(route)\n\n\troute.GET", "\n\troute.GET"},
		{"torr/btserver.go", "\tutils.PrefetchTrackers()", "\t// No unsolicited tracker-list download in the companion build."},
		{"torr/btserver.go", "\t\"context\"\n", ""},
		{"torr/btserver.go", "\t\"time\"\n", ""},
		{"torr/btserver.go", "\t\"github.com/anacrolix/publicip\"\n", ""},
		{"companion-torrent/socket.go", "\t\"github.com/anacrolix/missinggo\"", "\t\"github.com/anacrolix/missinggo\"\n\t\"github.com/anacrolix/torrent/companionguard\""},
		{"companion-torrent/socket.go", "dialer := net.Dialer{}", "dialer := companionguard.Dialer()"},
		{"companion-torrent/client.go", "\tdefer func() {\n\t\tif err != nil {\n\t\t\tcl = nil", "\tcompanionConfig(cfg)\n\tdefer func() {\n\t\tif err != nil {\n\t\t\tcl = nil"},
		{"companion-torrent/torrent.go", "\t\"github.com/anacrolix/torrent/metainfo\"", "\t\"github.com/anacrolix/torrent/metainfo\"\n\t\"github.com/anacrolix/torrent/companionguard/locator\""},
		{"companion-torrent/torrent.go", "func (t *Torrent) setInfoBytes(b []byte) error {", "func (t *Torrent) setInfoBytes(b []byte) error {\n\tif err := locator.ValidateInfo(b); err != nil { return err }"},
		{"companion-torrent/torrent.go", "bytes > 10000000", "bytes > locator.MaxBytes"},
		{"companion-torrent/tracker/http.go", "\t\"net\"\n", "\t\"github.com/anacrolix/torrent/companionguard\"\n"},
		{"companion-torrent/tracker/http.go", "\t\t\tDial: (&net.Dialer{\n\t\t\t\tTimeout: 15 * time.Second,\n\t\t\t}).Dial,", "\t\t\tDialContext: companionguard.Dialer().DialContext,"},
		{"companion-torrent/tracker/http.go", "\t\t\tProxy:               opt.HTTPProxy,", "\t\t\tProxy:               nil,"},
		{"companion-torrent/tracker/http.go", "\t\t\t\tInsecureSkipVerify: true,", "\t\t\t\tMinVersion: tls.VersionTLS12,"},
		{"companion-torrent/tracker/udp.go", "\t\"net/url\"", "\t\"net/url\"\n\t\"github.com/anacrolix/torrent/companionguard\""},
		{"companion-torrent/tracker/udp.go", "c.socket, err = net.Dial(c.dialNetwork(), hmp.String())", "c.socket, err = companionguard.TrackerDialer().DialContext(c.a.Context, \"udp4\", hmp.String())"},
		{"companion-torrent/tracker/udp.go", "func announceUDP(opt Announce, _url *url.URL) (AnnounceResponse, error) {", "func announceUDP(opt Announce, _url *url.URL) (AnnounceResponse, error) {\n\tif opt.UdpNetwork == \"udp6\" || _url.Scheme == \"udp6\" { return AnnounceResponse{}, companionguard.ErrDenied }\n\tctx := opt.Context\n\tif ctx == nil { ctx = context.Background() }\n\tctx, cancel := context.WithTimeout(ctx, 15*time.Second)\n\tdefer cancel()\n\topt.Context = ctx"},
	}
	for _, change := range changes {
		if err := edit(change[0], change[1], change[2]); err != nil {
			return err
		}
	}
	for _, name := range []string{"range.go", "range_test.go"} {
		data, err := os.ReadFile(filepath.Join(guard, "testdata", name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(source, "torr", "companion_"+name), data, 0600); err != nil {
			return err
		}
	}
	for _, name := range []string{"range_reader.go", "range_reader_test.go"} {
		data, err := os.ReadFile(filepath.Join(guard, "testdata", name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(source, "torr", "storage", "torrstor", "companion_"+name), data, 0600); err != nil {
			return err
		}
	}
	// This upstream discovery performs its own UDP DNS calls, outside HTTP.
	// Peers and trackers don't need the helper to discover a public address.
	bt, err := os.ReadFile(filepath.Join(source, "torr", "btserver.go"))
	if err != nil {
		return err
	}
	value := string(bt)
	begin := strings.Index(value, "\n\tlog.Println(\"Client config:\", settings.BTsets)")
	end := strings.Index(value, "\n}\n\nfunc (bt *BTServer) configureProxy()")
	if begin < 0 || end <= begin {
		return fmt.Errorf("public address discovery patch context changed")
	}
	if err := edit("torr/btserver.go", value[begin:end], "\n\t// Public address discovery is disabled in the companion build.\n"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(destination, "companionguard"), 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(guard)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".go") {
			data, err := fs.ReadFile(os.DirFS(guard), entry.Name())
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(destination, "companionguard", entry.Name()), data, 0600); err != nil {
				return err
			}
		}
	}
	for _, test := range [][2]string{{"socket_test.go", "companion_security_test.go"}, {"tracker_test.go", "tracker/companion_security_test.go"}, {"metadata_test.go", "companion_metadata_test.go"}} {
		data, err := os.ReadFile(filepath.Join(guard, "testdata", test[0]))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destination, filepath.FromSlash(test[1])), data, 0600); err != nil {
			return err
		}
	}
	if err := os.CopyFS(filepath.Join(destination, "companionguard", "locator"), os.DirFS(filepath.Join(guard, "..", "..", "internal", "locator"))); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(destination, "companion_config.go"), []byte(`package torrent

// These restrictions are mandatory, including after an engine settings reload.
func companionConfig(c *ClientConfig) {
	c.NoDHT = true
	c.PeriodicallyAnnounceTorrentsToDht = false
	c.DisableUTP = true
	c.DisablePEX = true
	c.DisableIPv6 = true
	c.LocalServiceDiscovery = nil
	c.NoDefaultPortForwarding = true
	c.ProxyURL = ""
	c.HTTPProxy = nil
}
`), 0600)
}

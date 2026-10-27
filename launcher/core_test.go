package main

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
}

func TestParseLibraryFolders(t *testing.T) {
	vdf := `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
		"apps" { "322170" "123" }
	}
	"1"
	{
		"path"		"D:\\SteamLibrary"
	}
}`
	got := ParseLibraryFolders(vdf)
	if len(got) != 2 || got[0] != `C:\Program Files (x86)\Steam` || got[1] != `D:\SteamLibrary` {
		t.Fatalf("got %q", got)
	}
}

func TestVersions(t *testing.T) {
	if CompareVersions("v5.11.1", "v5.9.0") <= 0 || CompareVersions("v0.2.1", "v0.2.1") != 0 || CompareVersions("v1.0.0-beta", "v1.0.1") >= 0 {
		t.Fatal("compare wrong")
	}
	rs := []Release{
		{Tag: "v6.0.0", Assets: []Asset{{Name: "geode-v6.0.0-win.zip"}}},
		{Tag: "v5.9.0", Assets: []Asset{{Name: "geode-v5.9.0-win.zip"}}},
		{Tag: "v5.12.0-beta.1", Prerelease: true, Assets: []Asset{{Name: "geode-v5.12.0-beta.1-win.zip"}}},
		{Tag: "v5.11.1", Assets: []Asset{{Name: "geode-v5.11.1-win.zip"}}},
		{Tag: "v5.11.2", Assets: []Asset{{Name: "geode-v5.11.2-mac.zip"}}},
	}
	r, err := PickGeode(rs, 5)
	if err != nil || r.Tag != "v5.11.1" {
		t.Fatalf("picked %v %v", r.Tag, err)
	}
}

func TestExtractZipStripAndTraversal(t *testing.T) {
	d := t.TempDir()
	z := filepath.Join(d, "r.zip")
	makeZip(t, z, map[string]string{"geode.loader/a.png": "A", "geode.loader/sub/b.png": "B"})
	out := filepath.Join(d, "out")
	if err := ExtractZip(z, out, "geode.loader"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "sub", "b.png")); string(b) != "B" {
		t.Fatal("strip failed")
	}
	z2 := filepath.Join(d, "r2.zip")
	makeZip(t, z2, map[string]string{"a.png": "A"})
	out2 := filepath.Join(d, "out2")
	if err := ExtractZip(z2, out2, "geode.loader"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out2, "a.png")); err != nil {
		t.Fatal("no-prefix zip failed")
	}
	z3 := filepath.Join(d, "evil.zip")
	makeZip(t, z3, map[string]string{"../evil.txt": "x"})
	if err := ExtractZip(z3, filepath.Join(d, "out3"), ""); err == nil {
		t.Fatal("traversal not blocked")
	}
}

func fakeSteamGD(t *testing.T, root string) string {
	gd := filepath.Join(root, "steamapps", "common", "Geometry Dash")
	write(t, filepath.Join(gd, "GeometryDash.exe"), "EXE")
	write(t, filepath.Join(gd, "libcocos2d.dll"), "DLL")
	write(t, filepath.Join(gd, "Resources", "GJ_GameSheet.png"), "PNG")
	write(t, filepath.Join(gd, "Geode.dll"), "USER GEODE")
	write(t, filepath.Join(gd, "XInput1_4.dll"), "PROXY")
	write(t, filepath.Join(gd, "geode", "mods", "km7dev.gdps-switcher.geode"), "SWITCHER")
	write(t, filepath.Join(gd, "steam_appid.txt"), "x")
	return gd
}

func TestCopyGame(t *testing.T) {
	d := t.TempDir()
	src := fakeSteamGD(t, filepath.Join(d, "Steam"))
	dst := filepath.Join(d, "game")
	if err := CopyGame(src, dst, nil); err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"Porotos2.exe", "libcocos2d.dll", "Resources/GJ_GameSheet.png"} {
		if _, err := os.Stat(filepath.Join(dst, must)); err != nil {
			t.Fatalf("missing %s", must)
		}
	}
	for _, not := range []string{"GeometryDash.exe", "Geode.dll", "XInput1_4.dll", "geode"} {
		if _, err := os.Stat(filepath.Join(dst, not)); err == nil {
			t.Fatalf("should not copy %s", not)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "steam_appid.txt")); string(b) != "322170" {
		t.Fatalf("steam_appid = %q", b)
	}
}

type fakePlat struct {
	steam    string
	launched string
}

func (f *fakePlat) SteamPath() string          { return f.steam }
func (f *fakePlat) SteamRunning() bool         { return true }
func (f *fakePlat) GameRunning() bool          { return false }
func (f *fakePlat) Launch(exe, _ string) error { f.launched = exe; return nil }
func (f *fakePlat) OpenFolder(string)          {}
func (f *fakePlat) OpenURL(string)             {}

func TestInstallAndPlayEndToEnd(t *testing.T) {
	d := t.TempDir()
	steam := filepath.Join(d, "Steam")
	fakeSteamGD(t, steam)

	assets := t.TempDir()
	makeZip(t, filepath.Join(assets, "geode-v5.11.1-win.zip"), map[string]string{"Geode.dll": "GEODE", "XInput1_4.dll": "PROXY", "GeodeUpdater.exe": "U"})
	makeZip(t, filepath.Join(assets, "resources.zip"), map[string]string{"logo.png": "L"})
	write(t, filepath.Join(assets, "porotos.geode"), "MOD v0.3.0")

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/repos/"+GeodeRepo+"/releases"):
			json.NewEncoder(w).Encode([]Release{{Tag: "v5.11.1", Assets: []Asset{
				{Name: "geode-v5.11.1-win.zip", URL: srv.URL + "/dl/geode-v5.11.1-win.zip"},
				{Name: "resources.zip", URL: srv.URL + "/dl/resources.zip"},
			}}})
		case strings.HasSuffix(r.URL.Path, "/repos/"+ModRepo+"/releases/latest"):
			json.NewEncoder(w).Encode(Release{Tag: "v0.3.0", HTMLURL: "x", Assets: []Asset{
				{Name: "valenbasiuk.porotos-core.geode", URL: srv.URL + "/dl/porotos.geode"},
				{Name: "Porotos2Launcher.exe", URL: srv.URL + "/dl/launcher.exe"},
			}})
		case strings.HasPrefix(r.URL.Path, "/dl/"):
			http.ServeFile(w, r, filepath.Join(assets, strings.TrimPrefix(r.URL.Path, "/dl/")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	githubAPI = srv.URL
	defer func() { githubAPI = "https://api.github.com" }()

	var mu sync.Mutex
	var events []string
	done := make(chan struct{}, 4)
	emit := func(ev string, p interface{}) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
		if ev == "launched" || ev == "error" {
			done <- struct{}{}
		}
		if ev == "error" {
			t.Errorf("error event: %v", p)
		}
	}
	plat := &fakePlat{steam: steam}
	paths := NewPaths(filepath.Join(d, "Porotos2Launcher"))
	app := NewApp(paths, plat, emit)
	app.Play()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}
	if plat.launched != filepath.Join(paths.Game, "Porotos2.exe") {
		t.Fatalf("launched %q", plat.launched)
	}
	checks := map[string]string{
		"Geode.dll":                             "GEODE",
		"geode/resources/geode.loader/logo.png": "L",
		"geode/mods/" + ModFileName:             "MOD v0.3.0",
	}
	for p, want := range checks {
		b, err := os.ReadFile(filepath.Join(paths.Game, p))
		if err != nil || string(b) != want {
			t.Fatalf("%s = %q (%v)", p, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(paths.Game, "geode", "mods", "km7dev.gdps-switcher.geode")); err == nil {
		t.Fatal("switcher should not be in the launcher copy")
	}
	st := LoadState(paths)
	if st.GeodeVersion != "v5.11.1" || st.ModVersion != "v0.3.0" || st.SourceSize == 0 {
		t.Fatalf("state %+v", st)
	}
	s := app.status(nil)
	if !s.Installed || s.NeedsUpdate || s.SteamChanged {
		t.Fatalf("status %+v", s)
	}

	// Steam actualiza GD: se avisa pero no se recopia solo.
	time.Sleep(1100 * time.Millisecond)
	write(t, filepath.Join(steam, "steamapps", "common", "Geometry Dash", "GeometryDash.exe"), "EXE NUEVO 2.209")
	if s := app.status(nil); !s.SteamChanged {
		t.Fatal("steam change not detected")
	}
	if b, _ := os.ReadFile(filepath.Join(paths.Game, "Porotos2.exe")); string(b) != "EXE" {
		t.Fatal("game copy should stay frozen")
	}
}

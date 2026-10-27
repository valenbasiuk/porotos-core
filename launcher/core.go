package main

// Lógica del launcher que no depende de Windows (se puede testear en cualquier OS).

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ModRepo       = "valenbasiuk/porotos-core"
	GeodeRepo     = "geode-sdk/geode"
	GeodeMajor    = 5 // Geode v5 = GD 2.2081
	ModFileName   = "valenbasiuk.porotos-core.geode"
	GameExeName   = "Porotos2.exe" // el save queda en %LOCALAPPDATA%\Porotos2
	SteamAppID    = "322170"
	SourceExeName = "GeometryDash.exe"
)

// Archivos/carpetas del GD de Steam que NO se copian (Geode y mods propios del usuario).
var copyExclude = map[string]bool{
	"geode":                true,
	"geode.dll":            true,
	"geode.pdb":            true,
	"xinput1_4.dll":        true,
	"geodeupdater.exe":     true,
	"geodeuninstaller.exe": true,
	"replays":              true,
	"steam_appid.txt":      true,
}

type State struct {
	SourceDir    string `json:"sourceDir"`
	SourceSize   int64  `json:"sourceSize"`
	SourceMtime  int64  `json:"sourceMtime"`
	GeodeVersion string `json:"geodeVersion"`
	ModVersion   string `json:"modVersion"`
	UpdatedAt    int64  `json:"updatedAt"`
}

type Paths struct {
	Data  string // %LOCALAPPDATA%\Porotos2Launcher
	Game  string // ...\game
	State string // ...\state.json
}

func NewPaths(dataDir string) Paths {
	return Paths{
		Data:  dataDir,
		Game:  filepath.Join(dataDir, "game"),
		State: filepath.Join(dataDir, "state.json"),
	}
}

func LoadState(p Paths) State {
	var s State
	b, err := os.ReadFile(p.State)
	if err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func SaveState(p Paths, s State) error {
	s.UpdatedAt = time.Now().Unix()
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.MkdirAll(p.Data, 0o755); err != nil {
		return err
	}
	return os.WriteFile(p.State, b, 0o644)
}

func GameInstalled(p Paths) bool {
	_, err := os.Stat(filepath.Join(p.Game, GameExeName))
	return err == nil
}

// ---------- Steam ----------

var vdfPathRe = regexp.MustCompile(`"path"\s+"([^"]+)"`)

// ParseLibraryFolders lee steamapps/libraryfolders.vdf y devuelve las carpetas de bibliotecas.
func ParseLibraryFolders(vdf string) []string {
	var out []string
	for _, m := range vdfPathRe.FindAllStringSubmatch(vdf, -1) {
		out = append(out, strings.ReplaceAll(m[1], `\\`, `\`))
	}
	return out
}

// FindGD busca "Geometry Dash" en la carpeta de Steam y en todas sus bibliotecas.
func FindGD(steamPath string) (string, error) {
	if steamPath == "" {
		return "", errors.New("no se encontró Steam")
	}
	libs := []string{steamPath}
	if b, err := os.ReadFile(filepath.Join(steamPath, "steamapps", "libraryfolders.vdf")); err == nil {
		libs = append(libs, ParseLibraryFolders(string(b))...)
	}
	for _, lib := range libs {
		dir := filepath.Join(lib, "steamapps", "common", "Geometry Dash")
		if _, err := os.Stat(filepath.Join(dir, SourceExeName)); err == nil {
			return dir, nil
		}
	}
	return "", errors.New("no se encontró Geometry Dash en Steam")
}

func SourceExeInfo(srcDir string) (int64, int64, error) {
	fi, err := os.Stat(filepath.Join(srcDir, SourceExeName))
	if err != nil {
		return 0, 0, err
	}
	return fi.Size(), fi.ModTime().Unix(), nil
}

// ---------- copia del juego ----------

type Progress func(stage string, done, total int64)

func listCopyFiles(src string) ([]string, int64, error) {
	var files []string
	var total int64
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return nil
		}
		top := strings.ToLower(strings.Split(filepath.ToSlash(rel), "/")[0])
		if copyExclude[top] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() {
			files = append(files, rel)
			total += info.Size()
		}
		return nil
	})
	return files, total, err
}

// CopyGame copia el GD de Steam a la carpeta del launcher, sin Geode ni mods,
// y renombra el exe para que el save quede separado del GD normal.
func CopyGame(src, dst string, prog Progress) error {
	files, total, err := listCopyFiles(src)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("la carpeta de GD está vacía")
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	var done int64
	for _, rel := range files {
		target := rel
		if strings.EqualFold(rel, SourceExeName) {
			target = GameExeName
		}
		n, err := copyFile(filepath.Join(src, rel), filepath.Join(dst, target))
		if err != nil {
			return fmt.Errorf("copiando %s: %w", rel, err)
		}
		done += n
		if prog != nil {
			prog("Copiando el juego", done, total)
		}
	}
	return os.WriteFile(filepath.Join(dst, "steam_appid.txt"), []byte(SteamAppID), 0o644)
}

func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return n, err
	}
	return n, os.Rename(tmp, dst)
}

// ---------- GitHub ----------

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type Release struct {
	Tag        string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	HTMLURL    string  `json:"html_url"`
	Assets     []Asset `json:"assets"`
}

func (r Release) Asset(match func(name string) bool) (Asset, bool) {
	for _, a := range r.Assets {
		if match(a.Name) {
			return a, true
		}
	}
	return Asset{}, false
}

var httpClient = &http.Client{Timeout: 10 * time.Minute}

// githubAPI se puede cambiar en los tests.
var githubAPI = "https://api.github.com"

func getJSON(url string, v interface{}) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Porotos2Launcher")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub respondió %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// LatestModRelease devuelve el último release publicado del mod (y del launcher).
func LatestModRelease() (Release, error) {
	var r Release
	err := getJSON(githubAPI+"/repos/"+ModRepo+"/releases/latest", &r)
	return r, err
}

// LatestGeodeRelease devuelve el último Geode estable de la versión mayor soportada.
func LatestGeodeRelease() (Release, error) {
	var rs []Release
	if err := getJSON(githubAPI+"/repos/"+GeodeRepo+"/releases?per_page=30", &rs); err != nil {
		return Release{}, err
	}
	return PickGeode(rs, GeodeMajor)
}

func PickGeode(rs []Release, major int) (Release, error) {
	var ok []Release
	for _, r := range rs {
		if r.Draft || r.Prerelease {
			continue
		}
		v := ParseVersion(r.Tag)
		if v[0] == major {
			if _, has := r.Asset(isGeodeWinZip); has {
				ok = append(ok, r)
			}
		}
	}
	if len(ok) == 0 {
		return Release{}, fmt.Errorf("no hay releases de Geode v%d", major)
	}
	sort.Slice(ok, func(i, j int) bool { return CompareVersions(ok[i].Tag, ok[j].Tag) > 0 })
	return ok[0], nil
}

func isGeodeWinZip(n string) bool {
	return strings.HasPrefix(n, "geode-") && strings.HasSuffix(n, "-win.zip")
}

func ParseVersion(s string) [3]int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	var v [3]int
	for i, part := range strings.SplitN(s, ".", 3) {
		n, _ := strconv.Atoi(part)
		v[i] = n
	}
	return v
}

func CompareVersions(a, b string) int {
	va, vb := ParseVersion(a), ParseVersion(b)
	for i := 0; i < 3; i++ {
		if va[i] != vb[i] {
			if va[i] > vb[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func Download(url, dst string, stage string, prog Progress) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Porotos2Launcher")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("descarga falló (%d)", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(tmp)
				return werr
			}
			done += int64(n)
			if prog != nil {
				prog(stage, done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			os.Remove(tmp)
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	os.Remove(dst)
	return os.Rename(tmp, dst)
}

// ExtractZip descomprime un zip en dstDir. Si todas las entradas comparten la carpeta
// raíz stripTop, se la saca (para resources.zip que puede venir con o sin "geode.loader/").
func ExtractZip(zipPath, dstDir, stripTop string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	strip := stripTop != ""
	if strip {
		for _, f := range zr.File {
			if !strings.HasPrefix(f.Name, stripTop+"/") {
				strip = false
				break
			}
		}
	}
	absDst, _ := filepath.Abs(dstDir)
	for _, f := range zr.File {
		name := f.Name
		if strip {
			name = strings.TrimPrefix(name, stripTop+"/")
		}
		if name == "" {
			continue
		}
		target := filepath.Join(dstDir, filepath.FromSlash(name))
		absT, _ := filepath.Abs(target)
		if absT != absDst && !strings.HasPrefix(absT, absDst+string(os.PathSeparator)) {
			return fmt.Errorf("ruta inválida en el zip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ---------- instalación de Geode y el mod ----------

func InstallGeode(p Paths, r Release, prog Progress) error {
	win, ok := r.Asset(isGeodeWinZip)
	if !ok {
		return errors.New("el release de Geode no trae la versión de Windows")
	}
	tmp := filepath.Join(p.Data, "tmp")
	zipPath := filepath.Join(tmp, win.Name)
	if err := Download(win.URL, zipPath, "Descargando Geode "+r.Tag, prog); err != nil {
		return err
	}
	defer os.Remove(zipPath)
	if err := ExtractZip(zipPath, p.Game, ""); err != nil {
		return err
	}
	if res, ok := r.Asset(func(n string) bool { return n == "resources.zip" }); ok {
		resZip := filepath.Join(tmp, "resources.zip")
		if err := Download(res.URL, resZip, "Descargando recursos de Geode", prog); err != nil {
			return err
		}
		defer os.Remove(resZip)
		dst := filepath.Join(p.Game, "geode", "resources", "geode.loader")
		os.RemoveAll(dst)
		if err := ExtractZip(resZip, dst, "geode.loader"); err != nil {
			return err
		}
	}
	return nil
}

func InstallMod(p Paths, r Release, prog Progress) error {
	a, ok := r.Asset(func(n string) bool { return strings.HasSuffix(n, ".geode") })
	if !ok {
		return errors.New("el release del mod no trae el archivo .geode")
	}
	return Download(a.URL, filepath.Join(p.Game, "geode", "mods", ModFileName), "Descargando Porotos 2 Core "+r.Tag, prog)
}

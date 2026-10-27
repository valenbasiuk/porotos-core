package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// Version se completa al compilar en GitHub (-ldflags "-X main.Version=v0.3.0").
var Version = "dev"

type Platform interface {
	SteamPath() string
	SteamRunning() bool
	GameRunning() bool
	Launch(exe, dir string) error
	OpenFolder(path string)
	OpenURL(url string)
}

type Status struct {
	Busy           bool   `json:"busy"`
	GDPath         string `json:"gdPath"`
	Installed      bool   `json:"installed"`
	GeodeVersion   string `json:"geodeVersion"`
	ModVersion     string `json:"modVersion"`
	LatestGeode    string `json:"latestGeode"`
	LatestMod      string `json:"latestMod"`
	NeedsUpdate    bool   `json:"needsUpdate"`
	SteamChanged   bool   `json:"steamChanged"`
	SteamRunning   bool   `json:"steamRunning"`
	LauncherUpdate string `json:"launcherUpdate"`
	ReleaseURL     string `json:"releaseUrl"`
	Offline        bool   `json:"offline"`
	Error          string `json:"error"`
	Version        string `json:"version"`
}

type App struct {
	paths Paths
	plat  Platform
	emit  func(event string, payload interface{})
	busy  atomic.Bool

	mu          sync.Mutex
	latestGeode *Release
	latestMod   *Release
}

func NewApp(paths Paths, plat Platform, emit func(string, interface{})) *App {
	return &App{paths: paths, plat: plat, emit: emit}
}

func (a *App) fetchLatest() error {
	g, gerr := LatestGeodeRelease()
	m, merr := LatestModRelease()
	a.mu.Lock()
	defer a.mu.Unlock()
	if gerr == nil {
		a.latestGeode = &g
	}
	if merr == nil {
		a.latestMod = &m
	}
	if gerr != nil {
		return gerr
	}
	return merr
}

func (a *App) status(fetchErr error) Status {
	s := Status{Busy: a.busy.Load(), Version: Version, SteamRunning: a.plat.SteamRunning()}
	st := LoadState(a.paths)
	s.Installed = GameInstalled(a.paths)
	s.GeodeVersion, s.ModVersion = st.GeodeVersion, st.ModVersion
	if gd, err := FindGD(a.plat.SteamPath()); err == nil {
		s.GDPath = gd
		if s.Installed && st.SourceSize != 0 {
			if size, mtime, err := SourceExeInfo(gd); err == nil {
				s.SteamChanged = size != st.SourceSize || mtime != st.SourceMtime
			}
		}
	} else if !s.Installed {
		s.Error = err.Error()
	}
	g, m := a.latest()
	if g != nil {
		s.LatestGeode = g.Tag
	}
	if m != nil {
		s.LatestMod = m.Tag
		s.ReleaseURL = m.HTMLURL
		if Version != "dev" && CompareVersions(m.Tag, Version) > 0 {
			if _, ok := m.Asset(func(n string) bool { return strings.HasSuffix(strings.ToLower(n), ".exe") }); ok {
				s.LauncherUpdate = m.Tag
			}
		}
	}
	s.Offline = fetchErr != nil && (g == nil || m == nil)
	s.NeedsUpdate = s.Installed && ((g != nil && st.GeodeVersion != g.Tag) || (m != nil && st.ModVersion != m.Tag))
	return s
}

// Refresh consulta GitHub y manda el estado a la UI.
func (a *App) Refresh() {
	go func() {
		err := a.fetchLatest()
		a.emit("status", a.status(err))
	}()
}

// RefreshLocal manda el estado sin consultar GitHub (para refrescar seguido).
func (a *App) RefreshLocal() {
	go a.emit("status", a.status(nil))
}

func (a *App) progress(stage string, done, total int64) {
	a.emit("progress", map[string]interface{}{"stage": stage, "done": done, "total": total})
}

// Install copia el juego (si hace falta o si force) y actualiza Geode y el mod.
func (a *App) Install(force bool) {
	if !a.busy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		err := a.install(force)
		a.busy.Store(false)
		if err != nil {
			a.emit("error", err.Error())
		} else {
			a.emit("done", "Listo")
		}
		a.emit("status", a.status(nil))
	}()
}

func (a *App) install(force bool) error {
	if a.plat.GameRunning() {
		return errors.New("cerrá Porotos 2 antes de actualizar")
	}
	st := LoadState(a.paths)
	if force || !GameInstalled(a.paths) {
		src, err := FindGD(a.plat.SteamPath())
		if err != nil {
			return fmt.Errorf("%v. Instalá Geometry Dash desde Steam", err)
		}
		// Se conservan geode/ (mods y config del jugador) si ya existía.
		if err := CopyGame(src, a.paths.Game, a.progress); err != nil {
			return err
		}
		size, mtime, _ := SourceExeInfo(src)
		st.SourceDir, st.SourceSize, st.SourceMtime = src, size, mtime
		st.GeodeVersion = "" // reinstalar Geode sobre la copia nueva
		if err := SaveState(a.paths, st); err != nil {
			return err
		}
	}
	gp, mp := a.latest()
	if gp == nil || mp == nil {
		err := a.fetchLatest()
		gp, mp = a.latest()
		if gp == nil || mp == nil {
			if st.GeodeVersion != "" && st.ModVersion != "" && fileExists(filepath.Join(a.paths.Game, "Geode.dll")) {
				return nil // sin internet pero ya instalado: se puede jugar igual
			}
			return fmt.Errorf("no se pudo conectar con GitHub: %v", err)
		}
	}
	g, m := *gp, *mp
	if st.GeodeVersion != g.Tag || !fileExists(filepath.Join(a.paths.Game, "Geode.dll")) {
		if err := InstallGeode(a.paths, g, a.progress); err != nil {
			return err
		}
		st.GeodeVersion = g.Tag
		if err := SaveState(a.paths, st); err != nil {
			return err
		}
	}
	if st.ModVersion != m.Tag || !fileExists(filepath.Join(a.paths.Game, "geode", "mods", ModFileName)) {
		if err := InstallMod(a.paths, m, a.progress); err != nil {
			return err
		}
		st.ModVersion = m.Tag
		if err := SaveState(a.paths, st); err != nil {
			return err
		}
	}
	return nil
}

// Play actualiza lo que haga falta y abre el juego.
func (a *App) Play() {
	if !a.busy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.busy.Store(false)
		if a.plat.GameRunning() {
			a.emit("error", "Porotos 2 ya está abierto")
			return
		}
		if !a.plat.SteamRunning() {
			a.emit("error", "Abrí Steam primero: el juego usa tu copia de Geometry Dash")
			return
		}
		if err := a.install(false); err != nil {
			a.emit("error", err.Error())
			a.emit("status", a.status(nil))
			return
		}
		exe := filepath.Join(a.paths.Game, GameExeName)
		if err := a.plat.Launch(exe, a.paths.Game); err != nil {
			a.emit("error", "No se pudo abrir el juego: "+err.Error())
			return
		}
		a.emit("launched", "")
	}()
}

func (a *App) OpenGameFolder() { a.plat.OpenFolder(a.paths.Game) }

func (a *App) OpenRelease() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.latestMod != nil {
		a.plat.OpenURL(a.latestMod.HTMLURL)
	} else {
		a.plat.OpenURL("https://github.com/" + ModRepo + "/releases/latest")
	}
}

func (a *App) latest() (*Release, *Release) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.latestGeode, a.latestMod
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

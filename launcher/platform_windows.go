//go:build windows

package main

import (
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type winPlatform struct{}

func (winPlatform) SteamPath() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE)
	if err == nil {
		defer k.Close()
		if p, _, err := k.GetStringValue("SteamPath"); err == nil && p != "" {
			return strings.ReplaceAll(p, "/", `\`)
		}
	}
	for _, root := range []registry.Key{registry.LOCAL_MACHINE} {
		for _, path := range []string{`SOFTWARE\WOW6432Node\Valve\Steam`, `SOFTWARE\Valve\Steam`} {
			k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			p, _, err := k.GetStringValue("InstallPath")
			k.Close()
			if err == nil && p != "" {
				return p
			}
		}
	}
	return `C:\Program Files (x86)\Steam`
}

func processRunning(name string) bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), name) {
			return true
		}
	}
	return false
}

func (winPlatform) SteamRunning() bool { return processRunning("steam.exe") }
func (winPlatform) GameRunning() bool  { return processRunning(GameExeName) }

func (winPlatform) Launch(exe, dir string) error {
	cmd := exec.Command(exe)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	return cmd.Start()
}

func shellOpen(target string) {
	verb, _ := windows.UTF16PtrFromString("open")
	t, _ := windows.UTF16PtrFromString(target)
	_ = windows.ShellExecute(0, verb, t, nil, nil, windows.SW_SHOWNORMAL)
}

func (winPlatform) OpenFolder(path string) { shellOpen(path) }
func (winPlatform) OpenURL(url string)     { shellOpen(url) }

func messageBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONERROR)
}

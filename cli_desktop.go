package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"skiff/internal/desktop"
)

// executable is the path of this program, links resolved; "" for a program
// built in a temporary folder (go run).
func executable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	if strings.HasPrefix(exe, os.TempDir()) {
		return ""
	}
	return exe
}

// installDesktopEntry gives the native window its name and icon in the
// menus and the dock of Linux desktops (see desktop.InstallEntry).
func installDesktopEntry(portable bool) {
	if !desktop.Native || portable {
		return
	}
	exe := executable()
	if exe == "" {
		return
	}
	if _, err := desktop.InstallEntry(appIcon, exe); err != nil {
		fmt.Fprintln(os.Stderr, "skiff: desktop entry:", err)
	}
}

// cmdDesktopEntry installs or removes the menu entry and the icon.
func cmdDesktopEntry(args []string) int {
	fl := flag.NewFlagSet("desktop-entry", flag.ContinueOnError)
	remove := fl.Bool("remove", false, "remove the menu entry and the icon Skiff installed")
	fl.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: skiff desktop-entry [-remove]\n\nInstalls the menu entry and the icon of Skiff for this user (Linux, BSD); the\nnative window does it by itself at its first start.")
		fl.PrintDefaults()
	}
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		fmt.Println("nothing to do: the icon is part of the program on this system")
		return 0
	}
	entry, icon := desktop.EntryPaths()
	if *remove {
		if err := desktop.RemoveEntry(); err != nil {
			fmt.Fprintln(os.Stderr, "skiff:", err)
			return 1
		}
		fmt.Println("removed", entry, "and", icon)
		return 0
	}
	exe := executable()
	if exe == "" {
		fmt.Fprintln(os.Stderr, "skiff: cannot tell where this program is")
		return 1
	}
	wrote, err := desktop.InstallEntry(appIcon, exe)
	if err != nil {
		fmt.Fprintln(os.Stderr, "skiff:", err)
		return 1
	}
	if wrote {
		fmt.Println("installed", entry, "and", icon)
	} else {
		fmt.Println("up to date:", entry)
	}
	return 0
}

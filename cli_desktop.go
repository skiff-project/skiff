package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"skiff/internal/config"
	"skiff/internal/desktop"
	"skiff/internal/urlhandler"
)

// executable is the path of this program, links resolved (the AppImage
// file inside an AppImage); "" for a program built in a temporary folder
// (go run).
func executable() string {
	exe, err := config.Executable()
	if err != nil {
		return ""
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
	if sys := desktop.SystemEntry(); sys != "" && !*remove {
		desktop.InstallEntry(appIcon, "") // removes an entry of ours that would hide it
		fmt.Println("nothing to do: the package of Skiff installed", sys)
		return 0
	}
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

// cmdURLHandler tells which applications open the sftp://, ftp://… links,
// or makes Skiff open them (the Windows installer runs it).
func cmdURLHandler(args []string) int {
	fl := flag.NewFlagSet("url-handler", flag.ContinueOnError)
	reg := fl.Bool("register", false, "open the links with Skiff")
	rem := fl.Bool("remove", false, "no longer open the links with Skiff")
	fl.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: skiff url-handler [-register | -remove]\n\nTells which application opens the %s:// links, or makes Skiff\nopen them (Preferences > System in the window).\n", strings.Join(urlhandler.Schemes, "://, "))
		fl.PrintDefaults()
	}
	if err := fl.Parse(args); err != nil {
		return 2
	}
	var err error
	switch {
	case *reg && *rem:
		fl.Usage()
		return 2
	case *reg:
		exe := executable()
		if exe == "" {
			fmt.Fprintln(os.Stderr, "skiff: cannot tell where this program is")
			return 1
		}
		err = urlhandler.Register(exe, appIcon)
	case *rem:
		err = urlhandler.Remove(executable())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "skiff:", err)
		return 1
	}
	st := urlhandler.Get()
	if !st.Supported {
		fmt.Println("links: not supported by this build")
		return 0
	}
	for _, x := range st.Schemes {
		h := x.Handler
		if h == "" {
			h = "(none)"
		}
		fmt.Printf("%-6s %s\n", x.Scheme+":", h)
	}
	return 0
}

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"skiff/frontend"
	"skiff/internal/config"
	"skiff/internal/core"
	"skiff/internal/desktop"
	"skiff/internal/instance"
	"skiff/internal/server"
)

func openStore() (*config.Store, error) {
	dir, portable := config.Dir()
	return config.Open(dir, portable)
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// cmdGUI starts the loopback server and shows the window. With a window
// already open on the same configuration folder, it gives that one the
// session URL instead, and ends. --link: the URL comes from a link clicked
// elsewhere (the system starts Skiff so), the window asks before connecting.
func cmdGUI(args []string) int {
	link := false
	var rest []string
	for _, a := range args {
		if a == "--link" || a == "-link" {
			link = true
			continue
		}
		rest = append(rest, a)
	}
	args = rest
	detachConsole()
	// one window per configuration folder (not while developing the UI)
	single := os.Getenv("SKIFF_UI_DIR") == ""
	if single {
		dir, _ := config.Dir()
		req := instance.Request{Link: link}
		if len(args) > 0 {
			req.URL = args[0]
		}
		allowForeground()
		if instance.Handoff(dir, req) {
			return 0
		}
	}
	store, err := openStore()
	if err != nil {
		fatalDialog("Cannot open the configuration: %v", err)
		return 1
	}
	c := core.New(store, core.Options{})
	defer c.Shutdown()

	token := randHex(32)
	if t := os.Getenv("SKIFF_DEV_TOKEN"); t != "" {
		token = t // development only: stable token across restarts
	}
	addr := "127.0.0.1:0"
	if p := os.Getenv("SKIFF_PORT"); p != "" {
		addr = "127.0.0.1:" + p
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fatalDialog("Cannot start the UI server: %v", err)
		return 1
	}
	port := ln.Addr().(*net.TCPAddr).Port
	static := frontend.Dist()
	if dir := os.Getenv("SKIFF_UI_DIR"); dir != "" {
		static = os.DirFS(dir) // development: serve the UI from disk
	}
	u := "http://127.0.0.1:" + strconv.Itoa(port) + "/?token=" + url.QueryEscape(token)
	handoff := ""
	if single {
		handoff = instance.NewKey()
	}
	srv := server.New(c, server.Options{
		Mode: server.Desktop, Version: version, Token: token, Static: static, Logf: log.Printf,
		HandoffKey: handoff, Icon: appIcon,
		Raise: func() {
			if c.Bus.Subscribers() == 0 {
				desktop.Reopen(u) // the browser tab was closed
			} else {
				desktop.Raise()
			}
		},
	})
	srv.SetPort(port)
	if len(args) > 0 {
		srv.SetStartupURL(args[0], link)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx, ln, "", "")
	unregister := func() {}
	if single {
		if unregister, err = instance.Register(store.Dir, port, handoff); err != nil {
			log.Printf("instance file: %v", err)
		}
	}
	defer unregister()

	if os.Getenv("SKIFF_PRINT_URL") != "" {
		fmt.Println(u)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		unregister()
		c.Shutdown()
		os.Exit(0)
	}()
	if os.Getenv("SKIFF_NO_WINDOW") != "" {
		// headless desktop mode (development, remote display): just serve.
		fmt.Println("Skiff UI:", u)
		select {}
	}
	installDesktopEntry(store.Portable)
	err = desktop.Run(desktop.Options{
		URL:     u,
		Title:   "Skiff",
		Version: version,
		Busy:    c.Transfers.Active,
		DataDir: filepath.Join(store.Dir, "webview"),
		Icon:    appIcon,
		OnURL:   func(link string) { srv.Open(instance.Request{URL: link, Link: true}) },
		WaitIdle: func() {
			fmt.Printf("Skiff is running at http://127.0.0.1:%d (press Ctrl+C to quit)\n", port)
			waitIdle(c)
		},
	})
	if err != nil {
		fatalDialog("%v", err)
		return 1
	}
	return 0
}

// waitIdle returns once a UI has connected and then no UI has been attached
// for a while (browser fallback).
func waitIdle(c *core.Core) {
	seen := false
	var goneSince time.Time
	for {
		time.Sleep(time.Second)
		n := c.Bus.Subscribers()
		if n > 0 {
			seen = true
			goneSince = time.Time{}
			continue
		}
		if !seen || c.Transfers.Active() > 0 {
			continue
		}
		if goneSince.IsZero() {
			goneSince = time.Now()
		} else if time.Since(goneSince) > 45*time.Second {
			return
		}
	}
}

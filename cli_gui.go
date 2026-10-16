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

// cmdGUI starts the loopback server and shows the window.
func cmdGUI(args []string) int {
	detachConsole()
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
	srv := server.New(c, server.Options{Mode: server.Desktop, Version: version, Token: token, Static: static, Logf: log.Printf})
	srv.SetPort(port)
	if len(args) > 0 {
		srv.SetStartupURL(args[0])
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx, ln, "", "")

	u := "http://127.0.0.1:" + strconv.Itoa(port) + "/?token=" + url.QueryEscape(token)
	if os.Getenv("SKIFF_PRINT_URL") != "" {
		fmt.Println(u)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
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

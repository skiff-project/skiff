package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/term"

	"skiff/internal/importer"
	"skiff/internal/keys"
)

func cmdKeygen(args []string) int {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	typ := fs.String("t", "ed25519", "key type: ed25519, rsa or ecdsa")
	bits := fs.Int("b", 0, "key size (rsa: 2048-8192, ecdsa: 256/384/521)")
	comment := fs.String("C", "", "comment")
	out := fs.String("f", "", "output file (default ~/.ssh/id_<type>)")
	ppk := fs.Bool("ppk", false, "write a PuTTY .ppk file instead of OpenSSH format")
	noPass := fs.Bool("N", false, "no passphrase")
	fs.Parse(args)
	if *out == "" {
		*out = "~/.ssh/id_" + *typ
		if *ppk {
			*out += ".ppk"
		}
	}
	if *comment == "" {
		u := os.Getenv("USER")
		if u == "" {
			u = os.Getenv("USERNAME")
		}
		h, _ := os.Hostname()
		*comment = u + "@" + h
	}
	pass := ""
	if !*noPass && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Print("Passphrase (empty for none): ")
		p1, _ := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Print("\nRepeat: ")
		p2, _ := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if string(p1) != string(p2) {
			fmt.Fprintln(os.Stderr, "passphrases differ")
			return 1
		}
		pass = string(p1)
	}
	format := "openssh"
	if *ppk {
		format = "ppk"
	}
	g, err := keys.Generate(keys.GenerateRequest{Type: *typ, Bits: *bits, Comment: *comment, Passphrase: pass, Format: format, SaveTo: *out})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Private key: %s\nFingerprint: %s\nPublic key:\n%s\n", g.Path, g.Fingerprint, g.Public)
	return 0
}

func cmdImport(args []string) int {
	if len(args) == 0 {
		fmt.Println("Usage: skiff import <source> [file]\n\nSources:")
		for _, s := range importer.Sources() {
			state := ""
			if s.Path != "" {
				state = " (" + s.Path + ")"
			}
			fmt.Printf("  %-16s %s%s\n", s.ID, s.Name, state)
		}
		return 2
	}
	path := ""
	if len(args) > 1 {
		path = args[1]
	}
	items, err := importer.Load(args[0], path, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	store, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	n := 0
	for _, it := range items {
		if _, err := store.ImportSite(it.Site, it.Password, it.Passphrase); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", it.Site.Title(), err)
			continue
		}
		note := ""
		if it.Note != "" {
			note = " — " + it.Note
		}
		fmt.Printf("  imported %s%s\n", it.Site.Title(), note)
		n++
	}
	fmt.Printf("%d site(s) imported.\n", n)
	return 0
}

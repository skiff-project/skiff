package main

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"skiff/internal/server"
)

//go:embed build/appicon.png
var appIcon []byte

// The licence of Skiff, its notice, and those of the software it includes
// (made by build/notices.py).
var (
	//go:embed LICENSE
	licenseText string
	//go:embed NOTICE
	noticeText string
	//go:embed THIRD_PARTY_NOTICES.txt
	thirdPartyText string
)

// fatalDialog reports a fatal error (stderr; a message box on Windows GUI).
func fatalDialog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, "skiff:", msg)
	showErrorBox("Skiff", msg)
}

func licences() server.Licences {
	return server.Licences{License: licenseText, Notice: noticeText, ThirdParty: thirdPartyText}
}

// cmdLicenses prints the licence of Skiff and those of the software it
// includes.
func cmdLicenses(args []string) int {
	if len(args) > 0 && (args[0] == "-full" || args[0] == "--full") {
		fmt.Print(noticeText, "\n", licenseText, "\n", thirdPartyText)
		return 0
	}
	fmt.Print(noticeText, "\n", thirdPartyText[:strings.Index(thirdPartyText, "\n=====")+1])
	fmt.Println("\nThe texts of these licences: skiff licenses -full")
	return 0
}

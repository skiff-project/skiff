package main

import (
	_ "embed"
	"fmt"
	"os"
)

//go:embed build/appicon.png
var appIcon []byte

// fatalDialog reports a fatal error (stderr; a message box on Windows GUI).
func fatalDialog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, "skiff:", msg)
	showErrorBox("Skiff", msg)
}

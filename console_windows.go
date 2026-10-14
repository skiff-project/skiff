//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procFreeConsole         = kernel32.NewProc("FreeConsole")
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procMessageBox          = user32.NewProc("MessageBoxW")
)

// detachConsole hides the console window when Skiff was started by
// double-clicking (the console then belongs to Skiff alone).
func detachConsole() {
	var pids [4]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), 4)
	if n == 1 {
		procFreeConsole.Call()
	}
}

func showErrorBox(title, msg string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(msg)
	procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10)
}

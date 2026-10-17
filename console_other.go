//go:build !windows

package main

func detachConsole()                 {}
func showErrorBox(title, msg string) {}
func allowForeground()               {}

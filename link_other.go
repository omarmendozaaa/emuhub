//go:build !linux && !windows

package main

// On macOS the scheme is declared in Info.plist (see wails.json) and links
// arrive through OnUrlOpen.
func registerScheme(string) error { return nil }

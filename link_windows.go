package main

import (
	"golang.org/x/sys/windows/registry"
)

// registerScheme registers emuhub:// for the current user, which needs no
// administrator rights. The release is a portable executable without an
// installer, so this happens on start.
func registerScheme(exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\emuhub`, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetStringValue("", "URL:EmuHub"); err != nil {
		return err
	}
	if err := k.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}
	cmd, _, err := registry.CreateKey(k, `shell\open\command`, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer cmd.Close()
	return cmd.SetStringValue("", `"`+exe+`" "%1"`)
}

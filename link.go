package main

import (
	"log"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Room links: the page a host's signaling server serves for a room link opens
// emuhub://join/…, which the system hands to EmuHub. A link that arrives
// while EmuHub runs reaches the open window through the single-instance
// lock; one that starts EmuHub waits until the interface asks for it.

const linkScheme = "emuhub://"

// roomLink returns the first emuhub:// link among a program's arguments.
func roomLink(args []string) string {
	for _, a := range args {
		if len(a) >= len(linkScheme) && strings.EqualFold(a[:len(linkScheme)], linkScheme) {
			return a
		}
	}
	return ""
}

// openLink receives a room link from the system.
func (a *App) openLink(link string) {
	if link == "" {
		return
	}
	a.mu.Lock()
	a.pendingLink = link
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return // the interface takes it once it starts
	}
	runtime.WindowUnminimise(ctx)
	runtime.WindowShow(ctx)
	runtime.EventsEmit(ctx, "room:link")
}

// secondInstance runs in the open EmuHub when the system starts another one,
// as it does for a room link.
func (a *App) secondInstance(data options.SecondInstanceData) {
	a.openLink(roomLink(data.Args))
}

// TakeRoomLink returns the room link EmuHub was asked to open, once.
func (a *App) TakeRoomLink() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	link := a.pendingLink
	a.pendingLink = ""
	return link
}

// registerLinks makes this EmuHub the program that opens emuhub:// links.
// It runs on every start, so the registration follows the executable when
// it moves.
func registerLinks() {
	exe, err := linkExecutable()
	if err != nil {
		log.Printf("enlaces de sala: %v", err)
		return
	}
	if err := registerScheme(exe); err != nil {
		log.Printf("enlaces de sala: no se pudo registrar emuhub://: %v", err)
	}
}

func linkExecutable() (string, error) {
	// An AppImage runs from a temporary mount; the image itself is what stays.
	if img := os.Getenv("APPIMAGE"); img != "" {
		return img, nil
	}
	return os.Executable()
}

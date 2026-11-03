package main

import (
	"embed"
	"log"
	"os"

	"emuhub/internal/library"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// WebKitGTK's DMA-BUF buffers crash with a Wayland protocol error on NVIDIA
	// drivers. Its renderer still works with shared-memory buffers, while
	// WEBKIT_DISABLE_DMABUF_RENDERER drops the UI to about 20 FPS. Composing with
	// Skia there flashes an old frame whenever the page changes.
	if _, err := os.Stat("/sys/module/nvidia"); err == nil &&
		os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" && os.Getenv("WEBKIT_DMABUF_RENDERER_FORCE_SHM") == "" {
		os.Setenv("WEBKIT_DMABUF_RENDERER_FORCE_SHM", "1")
		if os.Getenv("WEBKIT_USE_SKIA_FOR_COMPOSITION") == "" {
			os.Setenv("WEBKIT_USE_SKIA_FOR_COMPOSITION", "0")
		}
	}
	store, err := library.OpenStore()
	if err != nil {
		log.Fatalf("no se pudo abrir la biblioteca: %v", err)
	}
	app := NewApp(store)

	err = wails.Run(&options.App{
		Title:     "EmuHub",
		Width:     1440,
		Height:    860,
		MinWidth:  640,
		MinHeight: 400,
		// Without a maximum, Wails caps the window at the size of the monitor it
		// started on, so fullscreen on a wider monitor leaves part of it empty.
		MaxWidth:  16384,
		MaxHeight: 16384,
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Middleware: app.mediaMiddleware,
		},
		BackgroundColour: &options.RGBA{R: 8, G: 10, B: 28, A: 1},
		OnStartup:        app.startup,
		Bind:             []interface{}{app},
		Linux: &linux.Options{
			ProgramName:      "emuhub",
			WebviewGpuPolicy: linux.WebviewGpuPolicyAlways,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

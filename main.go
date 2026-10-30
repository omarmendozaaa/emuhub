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
	// WebKitGTK's DMA-BUF renderer crashes with a Wayland protocol error on
	// NVIDIA drivers; the fallback path is fast enough for this UI.
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" {
		os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
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

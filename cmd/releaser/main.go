// Command releaser writes latest.json, the file EmuHub reads to find and
// verify updates (see internal/update), from the program files of a
// release:
//
//	go run ./cmd/releaser -version v0.2.0 -merge old/latest.json -out dist/latest.json dist/EmuHub-*
//
// -merge keeps the platforms of an earlier latest.json of the same version
// that are not given now, so builds made by hand (macOS) can be added to a
// release CI already published.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"

	"emuhub/internal/download"
	"emuhub/internal/update"
)

func main() {
	version := flag.String("version", "", "versión publicada, como v0.2.0")
	merge := flag.String("merge", "", "latest.json anterior de esta versión (opcional)")
	out := flag.String("out", "latest.json", "archivo de salida")
	flag.Parse()

	if _, ok := update.ParseVersion(*version); !ok {
		log.Fatalf("versión no válida %q: usa vX.Y.Z", *version)
	}
	rel := update.Release{Version: *version, Files: map[string]update.File{}}
	if *merge != "" {
		if b, err := os.ReadFile(*merge); err == nil {
			var old update.Release
			if err := json.Unmarshal(b, &old); err != nil {
				log.Fatalf("%s: %v", *merge, err)
			}
			if old.Version == *version {
				for k, v := range old.Files {
					rel.Files[k] = v
				}
			}
		}
	}
	for _, path := range flag.Args() {
		name := filepath.Base(path)
		platform := update.PlatformOf(name)
		if platform == "" {
			continue // complete builds and anything else
		}
		st, err := os.Stat(path)
		if err != nil {
			log.Fatal(err)
		}
		sum, err := download.FileSHA256(path)
		if err != nil {
			log.Fatal(err)
		}
		rel.Files[platform] = update.File{Name: name, SHA256: sum, Size: st.Size()}
		log.Printf("%s: %s (%.1f MB)", platform, name, float64(st.Size())/1e6)
	}
	if len(rel.Files) == 0 {
		log.Fatal("ningún archivo de programa reconocido")
	}
	b, _ := json.MarshalIndent(rel, "", "  ")
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}

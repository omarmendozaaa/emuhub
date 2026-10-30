// Command bundler downloads the emulator releases pinned in
// emulators.lock.json for one platform, verifies their checksums, unpacks
// them and writes a single zip that gets embedded into EmuHub with
// `-tags bundle`.
//
// Nothing downloaded is executed: AppImages are unpacked by reading their
// squashfs payload directly.
package main

import (
	"archive/tar"
	"archive/zip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"emuhub/internal/bundle"

	"github.com/CalebQ42/squashfs"
	"github.com/bodgit/sevenzip"
	"github.com/ulikunitz/xz"
)

func main() {
	home, _ := os.UserHomeDir()
	lockPath := flag.String("lock", "emulators.lock.json", "lock file")
	platform := flag.String("platform", bundle.CurrentPlatform(), "linux-amd64, windows-amd64 o darwin-universal")
	cache := flag.String("cache", filepath.Join(home, ".cache", "emuhub-build"), "carpeta de caché")
	out := flag.String("out", "internal/bundle/data/emulators.zip", "zip de salida")
	flag.Parse()

	lock, err := bundle.ReadLock(*lockPath)
	if err != nil {
		log.Fatal(err)
	}

	stage := filepath.Join(*cache, "stage", *platform)
	if err := os.RemoveAll(stage); err != nil {
		log.Fatal(err)
	}
	manifest := bundle.Manifest{BundleVersion: lock.BundleVersion + "-" + *platform, Platform: *platform}

	for _, e := range lock.Emulators {
		p := e.Platforms[*platform]
		if p == nil {
			continue
		}
		log.Printf("%s %s", e.Name, e.Version)
		file, err := fetch(p.URL, p.SHA256, filepath.Join(*cache, "downloads"))
		if err != nil {
			log.Fatalf("%s: %v", e.ID, err)
		}
		dst := filepath.Join(stage, e.ID)
		if err := extract(file, p.Format, dst); err != nil {
			log.Fatalf("%s: extraer: %v", e.ID, err)
		}
		if err := checkCommand(e, *p, dst); err != nil {
			log.Fatalf("%s: %v", e.ID, err)
		}
		manifest.Emulators = append(manifest.Emulators, bundle.Emulator{
			ID: e.ID, Name: e.Name, Version: e.Version, Consoles: e.Consoles,
			License: e.License, Source: e.Source, Platform: *p,
		})
	}

	b, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), b, 0o644); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatal(err)
	}
	log.Printf("empaquetando %s", *out)
	if err := bundle.WriteZip(stage, *out); err != nil {
		log.Fatal(err)
	}
	if st, err := os.Stat(*out); err == nil {
		log.Printf("listo: %d emuladores, %.0f MB", len(manifest.Emulators), float64(st.Size())/1e6)
	}
}

// fetch downloads url into dir (reusing a cached copy) and verifies its
// SHA-256 against the lock file.
func fetch(url, want, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, filepath.Base(url))
	if sum, err := fileSHA256(path); err == nil && sum == want {
		return path, nil
	}
	log.Printf("  descargando %s", url)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("descarga: HTTP %d", resp.StatusCode)
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		return "", err
	}
	f.Close()
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(tmp)
		return "", fmt.Errorf("checksum no coincide: esperado %s, obtenido %s", want, got)
	}
	return path, os.Rename(tmp, path)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func extract(file, format, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	switch format {
	case "zip":
		zr, err := zip.OpenReader(file)
		if err != nil {
			return err
		}
		defer zr.Close()
		return bundle.ExtractZip(&zr.Reader, dst, nil)
	case "7z":
		return extract7z(file, dst)
	case "tar.xz":
		return extractTarXz(file, dst)
	case "appimage":
		return extractAppImage(file, dst)
	case "appimage-raw":
		// DwarFS-based AppImages can't be unpacked without running them;
		// ship the file as-is under a fixed name.
		src, err := os.Open(file)
		if err != nil {
			return err
		}
		defer src.Close()
		return writeFrom(src, filepath.Join(dst, "app.AppImage"), 0o755)
	}
	return fmt.Errorf("formato desconocido %q", format)
}

func extract7z(file, dst string) error {
	r, err := sevenzip.OpenReader(file)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		path, err := within(dst, f.Name)
		if err != nil {
			return err
		}
		mode := f.Mode()
		if mode.IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		if mode&fs.ModeSymlink != 0 {
			target, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}
			if err := os.Symlink(string(target), path); err != nil {
				return err
			}
			continue
		}
		err = writeFrom(rc, path, executablePerm(mode, f.Name))
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarXz(file, dst string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	xr, err := xz.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(xr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		path, err := within(dst, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(path, 0o755)
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(path), 0o755)
			err = os.Symlink(h.Linkname, path)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(path), 0o755)
			err = writeFrom(tr, path, fs.FileMode(h.Mode).Perm()|0o600)
		}
		if err != nil {
			return err
		}
	}
}

// extractAppImage reads the squashfs image appended to the AppImage runtime
// (it starts right after the ELF section headers).
func extractAppImage(file, dst string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	var hdr [64]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return err
	}
	if string(hdr[:4]) != "\x7fELF" || hdr[4] != 2 {
		return fmt.Errorf("no es un AppImage ELF de 64 bits")
	}
	shoff := binary.LittleEndian.Uint64(hdr[0x28:])
	shentsize := binary.LittleEndian.Uint16(hdr[0x3A:])
	shnum := binary.LittleEndian.Uint16(hdr[0x3C:])
	offset := int64(shoff) + int64(shentsize)*int64(shnum)

	r, err := squashfs.NewReaderAtOffset(f, offset)
	if err != nil {
		return fmt.Errorf("squashfs: %w", err)
	}
	return r.ExtractWithOptions(dst, squashfs.DefaultOptions())
}

func within(dst, name string) (string, error) {
	p := filepath.Join(dst, filepath.FromSlash(name))
	if !strings.HasPrefix(p, dst+string(os.PathSeparator)) {
		return "", fmt.Errorf("ruta inválida: %s", name)
	}
	return p, nil
}

// executablePerm keeps POSIX permissions when the archive has them; Windows
// archives only carry DOS attributes, which is fine since Windows ignores
// the executable bit.
func executablePerm(mode fs.FileMode, name string) fs.FileMode {
	return mode.Perm() | 0o600
}

func writeFrom(r io.Reader, path string, perm fs.FileMode) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// checkCommand makes sure the executable named in the command template
// exists in the unpacked emulator, catching lock-file typos at build time.
func checkCommand(e bundle.LockEntry, p bundle.Platform, root string) error {
	exe := strings.Fields(p.Command)[0]
	if !strings.Contains(exe, "{root}") {
		// e.g. macOS `open -a {root}/X.app`: check the .app argument instead.
		for _, f := range strings.Fields(p.Command) {
			if strings.Contains(f, "{root}") {
				exe = f
				break
			}
		}
	}
	path := filepath.FromSlash(strings.ReplaceAll(exe, "{root}", root))
	if _, err := os.Lstat(path); err != nil {
		return fmt.Errorf("el comando apunta a %s, que no existe en el paquete", path)
	}
	return nil
}

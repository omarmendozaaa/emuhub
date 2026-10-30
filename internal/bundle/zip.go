package bundle

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// WriteZip packs a directory tree, keeping executable bits and symlinks
// (macOS app bundles rely on framework symlinks).
func WriteZip(srcDir, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)

	err = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(srcDir, path)
		if rel == "." {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		switch {
		case info.IsDir():
			h.Name += "/"
			_, err = zw.CreateHeader(h)
			return err
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			h.Method = zip.Store
			w, err := zw.CreateHeader(h)
			if err != nil {
				return err
			}
			_, err = io.WriteString(w, target)
			return err
		case info.Mode().IsRegular():
			h.Method = zip.Deflate
			w, err := zw.CreateHeader(h)
			if err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(w, f)
			return err
		}
		return nil // skip devices, sockets, etc.
	})
	if err != nil {
		zw.Close()
		return err
	}
	return zw.Close()
}

// ExtractZip unpacks every entry of zr into dst. progress receives the bytes
// written so far and the total uncompressed size.
func ExtractZip(zr *zip.Reader, dst string, progress func(done, total int64)) error {
	var total, done int64
	for _, f := range zr.File {
		total += int64(f.UncompressedSize64)
	}
	for _, f := range zr.File {
		path, err := safeJoin(dst, f.Name)
		if err != nil {
			return err
		}
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		case mode&fs.ModeSymlink != 0:
			target, err := readAll(f)
			if err != nil {
				return err
			}
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.Remove(path)
			if err := os.Symlink(string(target), path); err != nil {
				return fmt.Errorf("symlink %s: %w", f.Name, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		perm := mode.Perm() | 0o600
		if err := writeFile(f, path, perm); err != nil {
			return err
		}
		done += int64(f.UncompressedSize64)
		if progress != nil {
			progress(done, total)
		}
	}
	return nil
}

func safeJoin(dst, name string) (string, error) {
	p := filepath.Join(dst, filepath.FromSlash(name))
	if p != dst && !strings.HasPrefix(p, dst+string(os.PathSeparator)) {
		return "", fmt.Errorf("ruta inválida en el paquete: %s", name)
	}
	return p, nil
}

func readAll(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func writeFile(f *zip.File, path string, perm fs.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

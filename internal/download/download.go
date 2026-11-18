// Package download fetches large files over HTTP, resuming interrupted
// downloads and verifying their size and SHA-256 before handing them over.
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// stallTimeout aborts a download that receives nothing for this long, which
// otherwise hangs forever on a dropped connection.
var stallTimeout = 60 * time.Second

var client = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ResponseHeaderTimeout: 30 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
}}

// File downloads url into dst. The data goes to dst+".part" first, so a
// download cut short continues where it stopped the next time. dst only
// appears once its size and SHA-256 match; an existing dst that already
// matches is kept without downloading. progress, if set, receives the bytes
// downloaded so far and the total.
func File(ctx context.Context, url, dst string, size int64, sha string, progress func(done, total int64)) error {
	sha = strings.ToLower(strings.TrimPrefix(sha, "sha256:"))
	if size <= 0 || len(sha) != 64 {
		return errors.New("descarga sin tamaño o suma de comprobación")
	}
	if st, err := os.Stat(dst); err == nil && st.Size() == size {
		if got, err := FileSHA256(dst); err == nil && got == sha {
			return nil
		}
	}
	part := dst + ".part"
	if err := fetch(ctx, url, part, size, progress); err != nil {
		return err
	}
	got, err := FileSHA256(part)
	if err != nil {
		return err
	}
	if got != sha {
		os.Remove(part)
		return errors.New("el archivo descargado está dañado (la suma de comprobación no coincide); vuelve a intentarlo")
	}
	return os.Rename(part, dst)
}

func fetch(ctx context.Context, url, part string, size int64, progress func(done, total int64)) error {
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	have, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if have > size {
		have = 0
	}
	if have == size {
		return nil
	}

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := client.Do(req)
	if err != nil {
		return friendly(ctx, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusPartialContent && have > 0:
	case resp.StatusCode == http.StatusOK:
		// The server ignored the range: start over.
		have = 0
	case resp.StatusCode == http.StatusNotFound:
		return errors.New("el archivo no está publicado (HTTP 404)")
	default:
		return fmt.Errorf("el servidor respondió HTTP %d", resp.StatusCode)
	}
	if err := f.Truncate(have); err != nil {
		return err
	}
	if _, err := f.Seek(have, io.SeekStart); err != nil {
		return err
	}

	stall := time.AfterFunc(stallTimeout, func() { cancel(errStalled) })
	defer stall.Stop()
	done := have
	buf := make([]byte, 256<<10)
	for done < size {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(stallTimeout)
			if int64(n) > size-done {
				return errors.New("el servidor envió más datos de los esperados")
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			if progress != nil {
				progress(done, size)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return friendly(ctx, rerr)
		}
	}
	if done != size {
		return errors.New("la descarga se cortó; vuelve a intentarlo y seguirá donde se quedó")
	}
	return f.Close()
}

var errStalled = errors.New("la descarga se quedó parada; vuelve a intentarlo y seguirá donde se quedó")

func friendly(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return fmt.Errorf("no se pudo descargar: %w", err)
}

// FileSHA256 returns the hex SHA-256 of a file.
func FileSHA256(path string) (string, error) {
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

// Get fetches a small document such as a release description, without
// waiting long for it.
func Get(ctx context.Context, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("no se pudo conectar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

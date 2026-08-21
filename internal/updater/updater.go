package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Asset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type Manifest struct {
	Version string           `json:"version"`
	Assets  map[string]Asset `json:"assets"`
}

func Run(ctx context.Context, manifestURL, component string) error {
	if component != "server" && component != "agent" {
		return errors.New("component must be server or agent")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("manifest: %s", resp.Status)
	}
	var m Manifest
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); e != nil {
		return e
	}
	key := component + "-" + runtime.GOOS + "-" + runtime.GOARCH
	a, ok := m.Assets[key]
	if !ok {
		return fmt.Errorf("no asset %s", key)
	}
	req, _ = http.NewRequestWithContext(ctx, "GET", a.URL, nil)
	resp, e = http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("asset: %s", resp.Status)
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	lock := exe + ".update.lock"
	lockFile, e := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return fmt.Errorf("another update may be running: %w", e)
	}
	lockFile.Close()
	defer os.Remove(lock)
	tmp := exe + ".new"
	defer os.Remove(tmp)
	f, e := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if e != nil {
		return e
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, a.Size+1))
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		os.Remove(tmp)
		return errors.New("asset checksum or size mismatch")
	}
	if runtime.GOOS == "linux" {
		return replaceManagedService(ctx, exe, tmp, component)
	}
	backup := exe + ".bak"
	os.Remove(backup)
	if e = os.Rename(exe, backup); e != nil {
		return e
	}
	if e = os.Rename(tmp, exe); e != nil {
		os.Rename(backup, exe)
		return e
	}
	return nil
}

func replaceManagedService(ctx context.Context, exe, staged, component string) error {
	unit := "routeglass.service"
	if component == "agent" {
		unit = "routeglass-agent.service"
	}
	if err := exec.CommandContext(ctx, "systemctl", "stop", unit).Run(); err != nil {
		return fmt.Errorf("stop %s before update: %w", unit, err)
	}
	backup := exe + ".bak"
	_ = os.Remove(backup)
	if err := os.Rename(exe, backup); err != nil {
		_ = exec.CommandContext(ctx, "systemctl", "start", unit).Run()
		return err
	}
	dbBackups, backupErr := backupDatabase(component)
	if backupErr != nil {
		_ = os.Rename(backup, exe)
		_ = exec.CommandContext(ctx, "systemctl", "start", unit).Run()
		return backupErr
	}
	rollback := func(cause error) error {
		_ = exec.CommandContext(context.Background(), "systemctl", "stop", unit).Run()
		_ = os.Remove(exe)
		_ = os.Rename(backup, exe)
		for original, saved := range dbBackups {
			_ = copyFile(saved, original, 0600)
			_ = os.Remove(saved)
		}
		startErr := exec.CommandContext(context.Background(), "systemctl", "start", unit).Run()
		if startErr != nil {
			return fmt.Errorf("%v; rollback restart failed: %w", cause, startErr)
		}
		return fmt.Errorf("update rolled back: %w", cause)
	}
	if err := os.Rename(staged, exe); err != nil {
		return rollback(err)
	}
	if err := exec.CommandContext(ctx, "systemctl", "start", unit).Run(); err != nil {
		return rollback(err)
	}
	if err := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", unit).Run(); err != nil {
		return rollback(errors.New("service did not become active"))
	}
	_ = os.Remove(backup)
	for _, saved := range dbBackups {
		_ = os.Remove(saved)
	}
	return nil
}

func backupDatabase(component string) (map[string]string, error) {
	backups := map[string]string{}
	if component != "server" {
		return backups, nil
	}
	for _, original := range []string{"/var/lib/routeglass/routeglass.db", "/var/lib/routeglass/routeglass.db-wal", "/var/lib/routeglass/routeglass.db-shm"} {
		if _, err := os.Stat(original); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		saved := original + ".update-backup"
		if err := copyFile(original, saved, 0600); err != nil {
			return nil, err
		}
		backups[original] = saved
	}
	return backups, nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	if !filepath.IsAbs(source) || !filepath.IsAbs(destination) || strings.TrimSpace(source) == "" {
		return errors.New("update copy path must be absolute")
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

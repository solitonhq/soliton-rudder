package plugins

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxPluginBytes = 100 << 20
	maxPluginFiles = 5000
)

// copyTree copies src into dst, skipping symlinks (they could point outside the plugin) and enforcing size limits.
func copyTree(src, dst string) error {
	var total int64
	var files int
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return os.MkdirAll(dst, 0o700)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o700)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		files++
		total += info.Size()
		if files > maxPluginFiles || total > maxPluginBytes {
			return errors.New("plugin is too large (limit 5000 files / 100 MB)")
		}
		mode := os.FileMode(0o600)
		if info.Mode()&0o111 != 0 {
			mode = 0o700
		}
		return copyFile(path, filepath.Join(dst, rel), mode)
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// extractZip unpacks a zip into dst, rejecting path traversal, symlinks and zip bombs.
func extractZip(zipPath, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("not a valid zip: %w", err)
	}
	defer zr.Close()
	if len(zr.File) > maxPluginFiles {
		return errors.New("zip contains too many files")
	}
	var total int64
	for _, f := range zr.File {
		name := filepath.Clean(f.Name)
		if !safeRel(name) {
			return fmt.Errorf("zip entry %q escapes the archive", f.Name)
		}
		target := filepath.Join(dst, name)
		if f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > maxPluginBytes {
			return errors.New("zip is too large when extracted (limit 100 MB)")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if f.Mode()&0o111 != 0 {
			mode = 0o700
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return err
		}
		n, err := io.Copy(out, io.LimitReader(rc, maxPluginBytes+1))
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
		if n > maxPluginBytes {
			return errors.New("zip entry is too large")
		}
	}
	return nil
}

// locateRoot finds the directory containing plugin.json (the root, or a single top-level folder).
func locateRoot(dir string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, "plugin.json")); err == nil {
		return dir, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && e.Name() != "__MACOSX" {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 1 {
		if _, err := os.Stat(filepath.Join(dir, dirs[0], "plugin.json")); err == nil {
			return filepath.Join(dir, dirs[0]), nil
		}
	}
	return "", errors.New("plugin.json not found (expected at the top level of the folder or zip)")
}

// stage makes a private, validated working copy of a plugin source (folder or .zip).
func stage(src, tmpRoot string) (dir string, man *Manifest, cleanup func(), err error) {
	if err = os.MkdirAll(tmpRoot, 0o700); err != nil {
		return
	}
	work, err := os.MkdirTemp(tmpRoot, "stage-*")
	if err != nil {
		return
	}
	cleanup = func() { os.RemoveAll(work) }
	fail := func(e error) (string, *Manifest, func(), error) {
		cleanup()
		return "", nil, func() {}, e
	}
	st, err := os.Stat(src)
	if err != nil {
		return fail(err)
	}
	raw := filepath.Join(work, "raw")
	if st.IsDir() {
		if err := copyTree(src, raw); err != nil {
			return fail(err)
		}
	} else if strings.HasSuffix(strings.ToLower(src), ".zip") {
		if err := extractZip(src, raw); err != nil {
			return fail(err)
		}
	} else {
		return fail(errors.New("choose a plugin folder or a .zip file"))
	}
	root, err := locateRoot(raw)
	if err != nil {
		return fail(err)
	}
	man, err = ReadManifest(root)
	if err != nil {
		return fail(err)
	}
	return root, man, cleanup, nil
}

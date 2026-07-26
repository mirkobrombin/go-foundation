package cpio

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func safeJoin(dst, name string) (string, error) {
	name = filepath.ToSlash(name)
	name = strings.TrimPrefix(name, "/")
	clean := filepath.Clean(name)
	if clean == "." || clean == "" {
		return "", errors.New("cpio: invalid name")
	}
	if strings.HasPrefix(clean, "..") || strings.Contains(clean, "../") {
		return "", errors.New("cpio: path traversal")
	}
	return filepath.Join(dst, filepath.FromSlash(clean)), nil
}

func permsFromMode(mode uint32) fs.FileMode {
	return fs.FileMode(mode & 0777)
}

// UnpackToDir unpacks a CPIO archive into dst.
func UnpackToDir(r io.Reader, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := rejectDestinationSymlinks(dst); err != nil {
		return err
	}

	rd := NewReader(r)
	for {
		e, err := rd.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		outPath, err := safeJoin(dst, e.Name)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dst, outPath)
		if err != nil {
			return err
		}

		isDir := (e.Mode & 0170000) == 0040000
		if isDir {
			if err := ensureDirectories(root, relative); err != nil {
				return err
			}
			continue
		}
		if (e.Mode & 0170000) != 0100000 {
			return errors.New("cpio: unsupported archive entry type")
		}

		if err := ensureDirectories(root, filepath.Dir(relative)); err != nil {
			return err
		}
		tempName, file, err := createTempFile(root, filepath.Dir(relative), permsFromMode(e.Mode))
		if err != nil {
			return err
		}
		if _, err := file.Write(e.Data); err != nil {
			file.Close()
			_ = root.Remove(tempName)
			return err
		}
		if err := file.Close(); err != nil {
			_ = root.Remove(tempName)
			return err
		}
		if err := root.Rename(tempName, relative); err != nil {
			_ = root.Remove(tempName)
			return err
		}
	}
}

func rejectDestinationSymlinks(dst string) error {
	return filepath.WalkDir(dst, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("cpio: destination contains symbolic link %q", path)
		}
		return nil
	})
}

func ensureDirectories(root *os.Root, relative string) error {
	clean := filepath.Clean(relative)
	if clean == "." {
		return nil
	}
	current := ""
	for _, component := range strings.Split(clean, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := root.Lstat(current)
		switch {
		case err == nil && info.Mode()&fs.ModeSymlink != 0:
			return fmt.Errorf("cpio: refusing symbolic link directory %q", current)
		case err == nil && !info.IsDir():
			return fmt.Errorf("cpio: path component %q is not a directory", current)
		case err == nil:
			continue
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
		if err := root.Mkdir(current, 0755); err != nil {
			return err
		}
	}
	return nil
}

func createTempFile(root *os.Root, directory string, mode fs.FileMode) (string, *os.File, error) {
	for range 100 {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, err
		}
		name := filepath.Join(directory, ".foundation-"+hex.EncodeToString(random[:])+".tmp")
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err == nil {
			return name, file, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, errors.New("cpio: cannot allocate temporary extraction file")
}

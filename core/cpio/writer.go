package cpio

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var hexDigits = [16]byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'A', 'B', 'C', 'D', 'E', 'F'}

func hex8To(dst []byte, v uint32) []byte {
	var out [8]byte
	for i := 7; i >= 0; i-- {
		out[i] = hexDigits[v&0xF]
		v >>= 4
	}
	return append(dst, out[:]...)
}

func writeNewcHeader(w io.Writer, ino, mode, uid, gid, nlink, mtime, filesize, devmaj, devmin, rdevmaj, rdevmin, namesize uint32) error {
	b := make([]byte, 0, 110)
	b = append(b, magicNewc...)
	b = hex8To(b, ino)
	b = hex8To(b, mode)
	b = hex8To(b, uid)
	b = hex8To(b, gid)
	b = hex8To(b, nlink)
	b = hex8To(b, mtime)
	b = hex8To(b, filesize)
	b = hex8To(b, devmaj)
	b = hex8To(b, devmin)
	b = hex8To(b, rdevmaj)
	b = hex8To(b, rdevmin)
	b = hex8To(b, namesize)
	b = hex8To(b, 0) // check
	_, err := w.Write(b)
	return err
}

func writePad4(w io.Writer, n uint32) error {
	p := pad4(n)
	if p == 0 {
		return nil
	}
	_, err := w.Write(make([]byte, p))
	return err
}

func normalizeName(name string) (string, error) {
	name = filepath.ToSlash(name)
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("cpio: empty name")
	}

	clean := filepath.Clean(name)
	clean = filepath.ToSlash(clean)
	clean = strings.TrimPrefix(clean, "./")
	if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", errors.New("cpio: invalid name")
	}
	return clean, nil
}

func (wr *Writer) AddDir(name string, mode fs.FileMode) error {
	if wr == nil || wr.closed {
		return errors.New("cpio: writer closed")
	}
	name, err := normalizeName(name)
	if err != nil {
		return err
	}
	if wr.entries >= wr.maxEntries {
		return errors.New("cpio: archive entry count exceeds limit")
	}
	perm := uint32(mode.Perm())
	cmode := uint32(0040000) | perm
	namesize := uint32(len(name) + 1)
	if err := writeNewcHeader(wr.w, wr.ino, cmode, wr.uid, wr.gid, 2, wr.mtime, 0, 0, 0, 0, 0, namesize); err != nil {
		return err
	}
	wr.ino++
	if _, err := io.WriteString(wr.w, name); err != nil {
		return err
	}
	if _, err := wr.w.Write([]byte{0}); err != nil {
		return err
	}
	if err := writePad4(wr.w, 110+namesize); err != nil {
		return err
	}
	wr.entries++
	return nil
}

func (wr *Writer) AddFile(name string, mode fs.FileMode, data []byte) error {
	if wr == nil || wr.closed {
		return errors.New("cpio: writer closed")
	}
	name, err := normalizeName(name)
	if err != nil {
		return err
	}
	size := uint64(len(data))
	if wr.entries >= wr.maxEntries {
		return errors.New("cpio: archive entry count exceeds limit")
	}
	if size > wr.maxFileSize {
		return errors.New("cpio: file data exceeds archive limit")
	}
	if size > wr.maxTotalSize-wr.totalSize {
		return errors.New("cpio: archive data exceeds total limit")
	}
	perm := uint32(mode.Perm())
	cmode := uint32(0100000) | perm
	filesize := uint32(len(data))
	namesize := uint32(len(name) + 1)
	if err := writeNewcHeader(wr.w, wr.ino, cmode, wr.uid, wr.gid, 1, wr.mtime, filesize, 0, 0, 0, 0, namesize); err != nil {
		return err
	}
	wr.ino++
	if _, err := io.WriteString(wr.w, name); err != nil {
		return err
	}
	if _, err := wr.w.Write([]byte{0}); err != nil {
		return err
	}
	if err := writePad4(wr.w, 110+namesize); err != nil {
		return err
	}
	if filesize > 0 {
		if _, err := wr.w.Write(data); err != nil {
			return err
		}
	}
	if err := writePad4(wr.w, filesize); err != nil {
		return err
	}
	wr.entries++
	wr.totalSize += size
	return nil
}

// Close writes the TRAILER!!! entry.
func (wr *Writer) Close() error {
	if wr == nil || wr.closed {
		return nil
	}
	wr.closed = true
	trail := "TRAILER!!!"
	namesize := uint32(len(trail) + 1)
	if err := writeNewcHeader(wr.w, wr.ino, 0, 0, 0, 1, wr.mtime, 0, 0, 0, 0, 0, namesize); err != nil {
		return err
	}
	if _, err := io.WriteString(wr.w, trail); err != nil {
		return err
	}
	if _, err := wr.w.Write([]byte{0}); err != nil {
		return err
	}
	return writePad4(wr.w, 110+namesize)
}

// PackDir packs an on-disk directory into a CPIO archive (deterministic order).
func PackDir(root string, w io.Writer, opts ...WriterOption) error {
	wr := NewWriter(w, opts...)
	root = filepath.Clean(root)
	source, err := os.OpenRoot(root)
	if err != nil {
		return errors.Join(err, wr.Close())
	}
	defer source.Close()

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}

		if d.IsDir() {
			return wr.AddDir(rel, 0755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return errors.New("cpio: symbolic links are not supported")
		}
		file, err := source.Open(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		info, statErr := file.Stat()
		if statErr != nil {
			file.Close()
			return statErr
		}
		if !info.Mode().IsRegular() {
			file.Close()
			return errors.New("cpio: special files are not supported")
		}
		if info.Size() < 0 || uint64(info.Size()) > defaultMaxFileSize {
			file.Close()
			return errors.New("cpio: file exceeds archive limit")
		}
		b, readErr := io.ReadAll(io.LimitReader(file, int64(defaultMaxFileSize)+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return errors.Join(readErr, closeErr)
		}
		if uint64(len(b)) > defaultMaxFileSize {
			return errors.New("cpio: file exceeds archive limit")
		}

		mode := fs.FileMode(0644)
		if info.Mode()&0100 != 0 {
			mode = 0755
		}
		return wr.AddFile(rel, mode, b)
	})
	return errors.Join(walkErr, wr.Close())
}

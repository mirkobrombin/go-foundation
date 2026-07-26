package cpio

import "io"

const (
	magicNewc = "070701"

	defaultMaxNameSize  = 4 << 10
	defaultMaxFileSize  = 64 << 20
	defaultMaxTotalSize = 512 << 20
	defaultMaxEntries   = 100_000
)

// Entry represents a single CPIO newc entry.
//
// The Data field is only set for regular files.
type Entry struct {
	Name     string
	Mode     uint32
	UID      uint32
	GID      uint32
	NLink    uint32
	MTime    uint32
	FileSize uint32
	Data     []byte
}

// IsTrailer reports whether the entry is the CPIO trailer.
func (e *Entry) IsTrailer() bool { return e != nil && e.Name == "TRAILER!!!" }

// Reader reads CPIO newc archives.
type Reader struct {
	r            io.Reader
	done         bool
	maxNameSize  uint64
	maxFileSize  uint64
	maxTotalSize uint64
	totalSize    uint64
	maxEntries   uint64
	entries      uint64
}

// WithReaderMaxEntries sets the maximum number of non-trailer entries.
func WithReaderMaxEntries(maxEntries uint64) ReaderOption {
	return func(reader *Reader) {
		reader.maxEntries = maxEntries
	}
}

// ReaderOption configures archive read limits.
type ReaderOption func(*Reader)

// WithReaderLimits sets maximum name, entry, and total data sizes.
func WithReaderLimits(maxNameSize, maxFileSize, maxTotalSize uint64) ReaderOption {
	return func(reader *Reader) {
		reader.maxNameSize = maxNameSize
		reader.maxFileSize = maxFileSize
		reader.maxTotalSize = maxTotalSize
	}
}

// NewReader creates a new CPIO reader from the given io.Reader.
func NewReader(r io.Reader, opts ...ReaderOption) *Reader {
	reader := &Reader{
		r:            r,
		maxNameSize:  defaultMaxNameSize,
		maxFileSize:  defaultMaxFileSize,
		maxTotalSize: defaultMaxTotalSize,
		maxEntries:   defaultMaxEntries,
	}
	for _, option := range opts {
		option(reader)
	}
	return reader
}

// Writer writes CPIO newc archives.
type Writer struct {
	w            io.Writer
	ino          uint32
	uid          uint32
	gid          uint32
	mtime        uint32
	closed       bool
	maxFileSize  uint64
	maxTotalSize uint64
	maxEntries   uint64
	totalSize    uint64
	entries      uint64
}

// WriterOption configures a Writer.
type WriterOption func(*Writer)

// WithMTimeUnix sets the mtime for the Writer.
func WithMTimeUnix(mtime uint32) WriterOption { return func(w *Writer) { w.mtime = mtime } }

// WithUIDGID sets the UID and GID for the Writer.
func WithUIDGID(uid, gid uint32) WriterOption {
	return func(w *Writer) {
		w.uid = uid
		w.gid = gid
	}
}

// WithWriterLimits sets maximum entry, total data, and entry count limits.
func WithWriterLimits(maxFileSize, maxTotalSize, maxEntries uint64) WriterOption {
	return func(w *Writer) {
		w.maxFileSize = maxFileSize
		w.maxTotalSize = maxTotalSize
		w.maxEntries = maxEntries
	}
}

// NewWriter creates a new CPIO newc Writer writing to w.
func NewWriter(w io.Writer, opts ...WriterOption) *Writer {
	wr := &Writer{
		w:            w,
		ino:          1,
		maxFileSize:  defaultMaxFileSize,
		maxTotalSize: defaultMaxTotalSize,
		maxEntries:   defaultMaxEntries,
	}
	for _, opt := range opts {
		opt(wr)
	}
	return wr
}

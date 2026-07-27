package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// The server cannot force a model to follow a procedure. What it can do is make
// a shortcut visible: it fingerprints the code it verified, hands back a receipt
// tied to that fingerprint, and tells every later call whether the workspace has
// moved since. An assistant that claims success without a current receipt is
// making a claim its own tools contradict, and the person reading can see it.

const receiptPrefix = "fv1"

// verificationState is what a caller is told about the standing of the code.
const (
	stateNone    = "never_verified"
	stateCurrent = "verified"
	stateStale   = "changed_since_verification"
)

type session struct {
	mu       sync.Mutex
	receipts map[string]string // directory -> fingerprint at verification
	written  map[string]bool   // directory -> code was written through this server
}

func newSession() *session {
	return &session{
		receipts: make(map[string]string),
		written:  make(map[string]bool),
	}
}

// Status is the verification standing of one directory.
type Status struct {
	State    string `json:"state"`
	Receipt  string `json:"receipt,omitempty"`
	Guidance string `json:"guidance"`
}

func (s *session) recordWrite(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written[dir] = true
}

func (s *session) recordVerification(dir, fingerprint string) string {
	receipt := receiptFor(dir, fingerprint)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receipts[dir] = fingerprint
	return receipt
}

func (s *session) clearVerification(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.receipts, dir)
}

// status reports whether the directory is verified as it currently stands.
func (s *session) status(dir string) Status {
	fingerprint, err := Fingerprint(dir)
	if err != nil {
		return Status{
			State:    stateNone,
			Guidance: "the workspace could not be fingerprinted, so nothing is verified; call foundation_verify",
		}
	}

	s.mu.Lock()
	recorded, ok := s.receipts[dir]
	written := s.written[dir]
	s.mu.Unlock()

	switch {
	case !ok && written:
		return Status{
			State:    stateNone,
			Guidance: "code was written in this session and never verified; call foundation_verify before reporting anything",
		}
	case !ok:
		return Status{
			State:    stateNone,
			Guidance: "this workspace has not been verified in this session; call foundation_verify before claiming it works",
		}
	case recorded != fingerprint:
		return Status{
			State:    stateStale,
			Guidance: "the code changed after the last verification, so the previous receipt is void; call foundation_verify again",
		}
	default:
		return Status{
			State:    stateCurrent,
			Receipt:  receiptFor(dir, fingerprint),
			Guidance: "quote this receipt when reporting the result; it is only valid for the code as it stands now",
		}
	}
}

func receiptFor(dir, fingerprint string) string {
	sum := sha256.Sum256([]byte(dir + "\x00" + fingerprint))
	return receiptPrefix + ":" + hex.EncodeToString(sum[:])[:16]
}

// Fingerprint hashes the Go sources and module files of a directory. Two
// workspaces with the same fingerprint hold the same code, which is what makes a
// receipt checkable rather than decorative.
func Fingerprint(dir string) (string, error) {
	resolved, err := resolveDir(dir)
	if err != nil {
		return "", err
	}

	var files []string
	err = filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", "node_modules", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".go") || name == "go.mod" || name == "go.sum" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("mcp: fingerprint %s: %w", resolved, err)
	}
	sort.Strings(files)

	hash := sha256.New()
	for _, path := range files {
		relative, err := filepath.Rel(resolved, path)
		if err != nil {
			return "", fmt.Errorf("mcp: fingerprint %s: %w", path, err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("mcp: fingerprint %s: %w", path, err)
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(relative), len(content))
		hash.Write(content)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ReceiptStanding is the answer to "is this receipt still good".
type ReceiptStanding struct {
	Receipt   string `json:"receipt"`
	Directory string `json:"directory"`
	Valid     bool   `json:"valid"`
	State     string `json:"state"`
	Verdict   string `json:"verdict"`
}

// CheckReceipt tells whether a receipt matches the code as it stands. It exists
// for the person reading an assistant's report: paste the receipt, learn whether
// the claim covers the code in front of you or an earlier version of it.
func CheckReceipt(dir, receipt string) (*ReceiptStanding, error) {
	resolved, err := resolveDir(dir)
	if err != nil {
		return nil, err
	}
	fingerprint, err := Fingerprint(resolved)
	if err != nil {
		return nil, err
	}
	expected := receiptFor(resolved, fingerprint)
	standing := &ReceiptStanding{
		Receipt:   strings.TrimSpace(receipt),
		Directory: resolved,
	}
	switch {
	case standing.Receipt == "":
		standing.State = stateNone
		standing.Verdict = "no receipt was given, so nothing about this workspace has been verified"
	case standing.Receipt == expected:
		standing.Valid = true
		standing.State = stateCurrent
		standing.Verdict = "the receipt matches the code as it stands"
	default:
		standing.State = stateStale
		standing.Verdict = "the receipt does not match this code; it was issued for a different state, or never issued at all"
	}
	return standing, nil
}

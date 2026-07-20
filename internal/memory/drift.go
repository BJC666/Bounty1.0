package memory

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sync"
)

// DriftDetector tracks file hashes to detect external modifications.
type DriftDetector struct {
	mu     sync.Mutex
	hashes map[string]string // path → sha256
}

func NewDriftDetector() *DriftDetector {
	return &DriftDetector{hashes: make(map[string]string)}
}

// Register records the current hash of a file.
func (d *DriftDetector) Register(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hashes[path] = hashContent(data)
	return nil
}

// Check returns true if a file has been modified externally since Register.
func (d *DriftDetector) Check(path string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	stored, ok := d.hashes[path]
	if !ok {
		return true // never registered = assume drifted
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return true // can't read = assume drifted
	}

	return hashContent(data) != stored
}

func hashContent(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

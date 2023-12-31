package bot

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// SampleUpdater represents a file that can be read and appended to.
// this is a helper for dynamic reloading of samples used by SpamFilter
type SampleUpdater struct {
	fileName string
}

// NewSampleUpdater creates a new SampleUpdater
func NewSampleUpdater(fileName string) *SampleUpdater {
	return &SampleUpdater{fileName: fileName}
}

// Reader returns a reader for the file, caller must close it
func (s *SampleUpdater) Reader() (io.ReadCloser, error) {
	fh, err := os.Open(s.fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", s.fileName, err)
	}
	return fh, nil
}

// Append a message to the file
func (s *SampleUpdater) Append(msg string) error {
	fh, err := os.OpenFile(s.fileName, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644) //nolint:gosec // keep it readable by all
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", s.fileName, err)
	}
	defer fh.Close()

	if _, err = fh.WriteString(strings.ReplaceAll(msg, "\n", " ") + "\n"); err != nil {
		return fmt.Errorf("failed to write to %s: %w", s.fileName, err)
	}
	return nil
}

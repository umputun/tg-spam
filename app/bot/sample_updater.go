package bot

import (
	"bufio"
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

// Append a message to the file, preventing duplicates
func (s *SampleUpdater) Append(msg string) error {
	// if file does not exist, create it
	if _, err := os.Stat(s.fileName); os.IsNotExist(err) {
		if _, err = os.Create(s.fileName); err != nil {
			return fmt.Errorf("failed to create %s: %w", s.fileName, err)
		}
	}

	// check if the message is already in the file
	fh, err := os.Open(s.fileName)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", s.fileName, err)
	}
	defer fh.Close()

	scanner := bufio.NewScanner(fh)
	for scanner.Scan() {
		// if a line matches the message, return right away
		if strings.EqualFold(strings.TrimSpace(scanner.Text()), strings.TrimSpace(msg)) {
			return nil
		}
	}

	// append the message to the file
	fh, err = os.OpenFile(s.fileName, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644) //nolint:gosec // keep it readable by all
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", s.fileName, err)
	}
	defer fh.Close()

	if _, err = fh.WriteString(strings.ReplaceAll(msg, "\n", " ") + "\n"); err != nil {
		return fmt.Errorf("failed to write to %s: %w", s.fileName, err)
	}
	return nil
}

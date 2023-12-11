package storage

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
)

// ApprovedUsers is a storage for approved users ids
type ApprovedUsers struct {
	filePath string
	file     *os.File
}

// NewApprovedUsers creates a new ApprovedUsers storage
func NewApprovedUsers(filePath string) *ApprovedUsers {
	return &ApprovedUsers{
		filePath: filePath,
	}
}

// Store saves ids to the storage, overwriting the existing content
// data is stored in little endian, binary format. 8 bytes for each id.
func (au *ApprovedUsers) Store(ids []int64) error {
	log.Printf("storing %d ids to %s", len(ids), au.filePath)
	file, err := os.Create(au.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, id := range ids {
		if err := binary.Write(file, binary.LittleEndian, id); err != nil {
			return fmt.Errorf("failed to write id %d: %w", id, err)
		}
	}

	return nil
}

// Read reads ids from the storage
// Each read returns one id, followed by a newline
func (au *ApprovedUsers) Read(p []byte) (n int, err error) {
	if au.file == nil {
		file, e := os.Open(au.filePath)
		if e != nil {
			return 0, e
		}
		au.file = file
	}

	var id int64
	if err = binary.Read(au.file, binary.LittleEndian, &id); err != nil {
		if err == io.EOF {
			_ = au.file.Close() // we don't care about the error here, read-only
			au.file = nil
		}
		return 0, err
	}

	idBytes := []byte(fmt.Sprintf("%d\n", id))
	n = copy(p, idBytes)
	return n, nil
}

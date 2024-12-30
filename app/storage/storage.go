package storage

import (
	"context"
	"io"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver loaded here
)

// NewSqliteDB creates a new sqlite database
func NewSqliteDB(file string) (*sqlx.DB, error) {
	return sqlx.Connect("sqlite", file)
}

func setSqlitePragma(db *sqlx.DB) error {
	// set pragmas for better concurrency support
	pragmas := map[string]string{
		"journal_mode": "WAL",
		"synchronous":  "NORMAL",
		"busy_timeout": "5000",
		"foreign_keys": "ON",
	}

	// set pragma
	for name, value := range pragmas {
		if _, err := db.Exec("PRAGMA " + name + " = " + value); err != nil {
			return err
		}
	}
	return nil
}

// SampleUpdater is a service to update dynamic (user's) samples for detector, either ham or spam.
type SampleUpdater struct {
	SamplesService *Samples
	SampleType     SampleType
	Timeout        time.Duration
}

// NewSampleUpdater creates a new SampleUpdater
func NewSampleUpdater(samplesService *Samples, sampleType SampleType, timeout time.Duration) *SampleUpdater {
	return &SampleUpdater{SamplesService: samplesService, SampleType: sampleType, Timeout: timeout}
}

// Append a message to the samples, forcing user origin
func (u *SampleUpdater) Append(msg string) error {
	ctx, cancel := context.Background(), func() {}
	if u.Timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), u.Timeout)
	}
	defer cancel()
	return u.SamplesService.Add(ctx, u.SampleType, SampleOriginUser, msg)
}

// Reader returns a reader for the samples
func (u *SampleUpdater) Reader() (io.ReadCloser, error) {
	// we don't want to pass context with timeout here, as it's an async operation
	return u.SamplesService.Reader(context.Background(), u.SampleType, SampleOriginUser)
}

// Package storage provides a storage engine for sql databases.
// The storage engine is a wrapper around sqlx.DB with additional functionality to work with the various types of database engines.
// Each table is represented by a struct, and each struct has a method to work the table with business logic for this data type.
package storage

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver loaded here

	"github.com/umputun/tg-spam/lib/approved"
)

// EngineType is a type of database engine
type EngineType string

// enum of supported database engines
const (
	EngineTypeUnknown EngineType = ""
	EngineTypeSqlite  EngineType = "sqlite"
)

// Engine is a wrapper for sqlx.DB with type.
// EngineType allows distinguishing between different database engines.
type Engine struct {
	sqlx.DB
	RWLocker
	dbType EngineType // type of the database engine
}

// NewSqliteDB creates a new sqlite database
func NewSqliteDB(file string) (*Engine, error) {
	db, err := sqlx.Connect("sqlite", file)
	if err != nil {
		return &Engine{}, err
	}
	if err := setSqlitePragma(db); err != nil {
		return &Engine{}, err
	}
	return &Engine{DB: *db, dbType: EngineTypeSqlite, RWLocker: &sync.RWMutex{}}, nil
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
// The service is used by the detector to update the samples.
type SampleUpdater struct {
	samplesService *Samples
	sampleType     SampleType
	timeout        time.Duration
	gid            string
}

// NewSampleUpdater creates a new SampleUpdater
func NewSampleUpdater(gid string, samplesService *Samples, sampleType SampleType, timeout time.Duration) *SampleUpdater {
	return &SampleUpdater{gid: gid, samplesService: samplesService, sampleType: sampleType, timeout: timeout}
}

// Append a message to the samples, forcing user origin
func (u *SampleUpdater) Append(msg string) error {
	ctx, cancel := context.Background(), func() {}
	if u.timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), u.timeout)
	}
	defer cancel()
	return u.samplesService.Add(ctx, u.gid, u.sampleType, SampleOriginUser, msg)
}

// Reader returns a reader for the samples
func (u *SampleUpdater) Reader() (io.ReadCloser, error) {
	// we don't want to pass context with timeout here, as it's an async operation
	return u.samplesService.Reader(context.Background(), u.gid, u.sampleType, SampleOriginUser)
}

// RWLocker is a read-write locker interface
type RWLocker interface {
	sync.Locker
	RLock()
	RUnlock()
}

// PinnedGIDApprovedUsers is a storage for approved users for a given GID
// needed for consumers without support of per-call gid parameter, like Detector
type PinnedGIDApprovedUsers struct {
	au  *ApprovedUsers
	gid string
}

// NewPinnedGIDApprovedUsers creates a new PinnedGIDApprovedUsers
func NewPinnedGIDApprovedUsers(au *ApprovedUsers, gid string) *PinnedGIDApprovedUsers {
	return &PinnedGIDApprovedUsers{au: au, gid: gid}
}

// Read returns a list of all approved users for the pinned GID
func (au *PinnedGIDApprovedUsers) Read(ctx context.Context) ([]approved.UserInfo, error) {
	return au.au.Read(ctx, au.gid)
}

// Write adds a new approved user for the pinned GID
func (au *PinnedGIDApprovedUsers) Write(ctx context.Context, user approved.UserInfo) error {
	return au.au.Write(ctx, user)
}

// Delete removes an approved user for the pinned GID
func (au *PinnedGIDApprovedUsers) Delete(ctx context.Context, id string) error {
	return au.au.Delete(ctx, id)
}

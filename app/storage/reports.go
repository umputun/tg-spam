package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/umputun/tg-spam/app/storage/engine"
)

// Reports is a storage for user spam reports
type Reports struct {
	*engine.SQL
	engine.RWLocker
}

// Report represents a spam report from a user
type Report struct {
	ID               int64     `db:"id"`
	MsgID            int       `db:"msg_id"`
	ChatID           int64     `db:"chat_id"`
	GID              string    `db:"gid"`
	ReporterUserID   int64     `db:"reporter_user_id"`
	ReporterUserName string    `db:"reporter_user_name"`
	ReportedUserID   int64     `db:"reported_user_id"`
	ReportedUserName string    `db:"reported_user_name"`
	MsgText          string    `db:"msg_text"`
	ReportTime       time.Time `db:"report_time"`
	NotificationSent bool      `db:"notification_sent"`
	AdminMsgID       int       `db:"admin_msg_id"`
}

// reports-related command constants
const (
	CmdCreateReportsTable engine.DBCmd = iota + 500
	CmdCreateReportsIndexes
	CmdAddReport
	CmdGetReportsByMessage
	CmdGetReporterCountSince
	CmdUpdateReportsAdminMsgID
	CmdDeleteReporter
	CmdDeleteReportsByMessage
)

// reportsQueries holds all reports-related queries
var reportsQueries = engine.NewQueryMap().
	Add(CmdCreateReportsTable, engine.Query{
		Sqlite: `CREATE TABLE IF NOT EXISTS reports (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            msg_id INTEGER,
            chat_id INTEGER,
            gid TEXT NOT NULL DEFAULT '',
            reporter_user_id INTEGER,
            reporter_user_name TEXT,
            reported_user_id INTEGER,
            reported_user_name TEXT,
            msg_text TEXT,
            report_time TIMESTAMP,
            notification_sent BOOLEAN DEFAULT 0,
            admin_msg_id INTEGER DEFAULT 0,
            UNIQUE(gid, msg_id, chat_id, reporter_user_id)
        )`,
		Postgres: `CREATE TABLE IF NOT EXISTS reports (
            id SERIAL PRIMARY KEY,
            msg_id INTEGER,
            chat_id BIGINT,
            gid TEXT NOT NULL DEFAULT '',
            reporter_user_id BIGINT,
            reporter_user_name TEXT,
            reported_user_id BIGINT,
            reported_user_name TEXT,
            msg_text TEXT,
            report_time TIMESTAMP,
            notification_sent BOOLEAN DEFAULT false,
            admin_msg_id INTEGER DEFAULT 0,
            UNIQUE(gid, msg_id, chat_id, reporter_user_id)
        )`,
	}).
	AddSame(CmdCreateReportsIndexes, `
        CREATE INDEX IF NOT EXISTS idx_reports_msg_chat_gid ON reports(gid, msg_id, chat_id);
        CREATE INDEX IF NOT EXISTS idx_reports_admin_msg ON reports(admin_msg_id);
        CREATE INDEX IF NOT EXISTS idx_reports_reporter_time ON reports(reporter_user_id, report_time);
        CREATE INDEX IF NOT EXISTS idx_reports_gid_time ON reports(gid, report_time DESC);
    `).
	Add(CmdAddReport, engine.Query{
		Sqlite: "INSERT OR IGNORE INTO reports (msg_id, chat_id, gid, reporter_user_id, reporter_user_name, " +
			"reported_user_id, reported_user_name, msg_text, report_time, notification_sent, admin_msg_id) " +
			"VALUES (:msg_id, :chat_id, :gid, :reporter_user_id, :reporter_user_name, :reported_user_id, " +
			":reported_user_name, :msg_text, :report_time, :notification_sent, :admin_msg_id)",
		Postgres: "INSERT INTO reports (msg_id, chat_id, gid, reporter_user_id, reporter_user_name, " +
			"reported_user_id, reported_user_name, msg_text, report_time, notification_sent, admin_msg_id) " +
			"VALUES (:msg_id, :chat_id, :gid, :reporter_user_id, :reporter_user_name, :reported_user_id, " +
			":reported_user_name, :msg_text, :report_time, :notification_sent, :admin_msg_id) " +
			"ON CONFLICT (gid, msg_id, chat_id, reporter_user_id) DO NOTHING",
	}).
	AddSame(CmdGetReportsByMessage, "SELECT * FROM reports WHERE gid = ? AND msg_id = ? AND chat_id = ? ORDER BY report_time ASC").
	AddSame(CmdGetReporterCountSince, "SELECT COUNT(*) FROM reports WHERE gid = ? AND reporter_user_id = ? AND report_time > ?").
	AddSame(CmdUpdateReportsAdminMsgID,
		"UPDATE reports SET notification_sent = true, admin_msg_id = ? WHERE gid = ? AND msg_id = ? AND chat_id = ?").
	AddSame(CmdDeleteReporter, "DELETE FROM reports WHERE gid = ? AND reporter_user_id = ? AND msg_id = ? AND chat_id = ?").
	AddSame(CmdDeleteReportsByMessage, "DELETE FROM reports WHERE gid = ? AND msg_id = ? AND chat_id = ?")

// NewReports creates a new Reports storage
func NewReports(ctx context.Context, db *engine.SQL) (*Reports, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}
	res := &Reports{SQL: db, RWLocker: db.MakeLock()}
	cfg := engine.TableConfig{
		Name:          "reports",
		CreateTable:   CmdCreateReportsTable,
		CreateIndexes: CmdCreateReportsIndexes,
		MigrateFunc:   res.migrate,
		QueriesMap:    reportsQueries,
	}
	if err := engine.InitTable(ctx, db, cfg); err != nil {
		return nil, fmt.Errorf("failed to init reports storage: %w", err)
	}
	return res, nil
}

// migrate is a no-op migration function for reports table (new table, no migration needed)
func (r *Reports) migrate(_ context.Context, _ *sqlx.Tx, _ string) error {
	// no migration needed for new table
	return nil
}

// Add adds a new spam report to the database
func (r *Reports) Add(ctx context.Context, report Report) error {
	r.Lock()
	defer r.Unlock()

	log.Printf("[DEBUG] add report: msgID:%d, chatID:%d, reporter:%d, reported:%d",
		report.MsgID, report.ChatID, report.ReporterUserID, report.ReportedUserID)

	// set auto-populated fields
	report.GID = r.GID()
	if report.ReportTime.IsZero() {
		report.ReportTime = time.Now()
	}
	report.NotificationSent = false
	report.AdminMsgID = 0

	query, err := reportsQueries.Pick(r.Type(), CmdAddReport)
	if err != nil {
		return fmt.Errorf("failed to get insert query: %w", err)
	}

	if _, err := r.NamedExecContext(ctx, query, report); err != nil {
		return fmt.Errorf("failed to insert report: %w", err)
	}

	log.Printf("[INFO] report added: msgID:%d, reporter:%s (%d), reported:%s (%d)",
		report.MsgID, report.ReporterUserName, report.ReporterUserID, report.ReportedUserName, report.ReportedUserID)

	// cleanup old reports that never reached threshold
	if err := r.cleanupOldReports(ctx); err != nil {
		log.Printf("[WARN] failed to cleanup old reports: %v", err)
	}

	return nil
}

// GetByMessage returns all reports for a specific message
func (r *Reports) GetByMessage(ctx context.Context, msgID int, chatID int64) ([]Report, error) {
	r.RLock()
	defer r.RUnlock()

	query, err := reportsQueries.Pick(r.Type(), CmdGetReportsByMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to get query: %w", err)
	}
	query = r.Adopt(query)

	var reports []Report
	if err := r.SelectContext(ctx, &reports, query, r.GID(), msgID, chatID); err != nil {
		return nil, fmt.Errorf("failed to get reports: %w", err)
	}

	return reports, nil
}

// GetReporterCountSince returns the number of reports made by a specific user since the given time
func (r *Reports) GetReporterCountSince(ctx context.Context, reporterID int64, since time.Time) (int, error) {
	r.RLock()
	defer r.RUnlock()

	query, err := reportsQueries.Pick(r.Type(), CmdGetReporterCountSince)
	if err != nil {
		return 0, fmt.Errorf("failed to get query: %w", err)
	}
	query = r.Adopt(query)

	var count int
	if err := r.GetContext(ctx, &count, query, r.GID(), reporterID, since); err != nil {
		return 0, fmt.Errorf("failed to get reporter count: %w", err)
	}

	return count, nil
}

// UpdateAdminMsgID updates the admin message ID and sets notification_sent flag for all reports of a specific message
func (r *Reports) UpdateAdminMsgID(ctx context.Context, msgID int, chatID int64, adminMsgID int) error {
	r.Lock()
	defer r.Unlock()

	query, err := reportsQueries.Pick(r.Type(), CmdUpdateReportsAdminMsgID)
	if err != nil {
		return fmt.Errorf("failed to get query: %w", err)
	}
	query = r.Adopt(query)

	if _, err := r.ExecContext(ctx, query, adminMsgID, r.GID(), msgID, chatID); err != nil {
		return fmt.Errorf("failed to update admin message id: %w", err)
	}

	log.Printf("[INFO] admin message id %d set for reports of msgID:%d, chatID:%d", adminMsgID, msgID, chatID)
	return nil
}

// DeleteReporter removes a specific reporter from reports for a given message
func (r *Reports) DeleteReporter(ctx context.Context, reporterID int64, msgID int, chatID int64) error {
	r.Lock()
	defer r.Unlock()

	query, err := reportsQueries.Pick(r.Type(), CmdDeleteReporter)
	if err != nil {
		return fmt.Errorf("failed to get query: %w", err)
	}
	query = r.Adopt(query)

	if _, err := r.ExecContext(ctx, query, r.GID(), reporterID, msgID, chatID); err != nil {
		return fmt.Errorf("failed to delete reporter: %w", err)
	}

	log.Printf("[INFO] reporter %d removed from report msgID:%d, chatID:%d", reporterID, msgID, chatID)
	return nil
}

// DeleteByMessage deletes all reports for a specific message
func (r *Reports) DeleteByMessage(ctx context.Context, msgID int, chatID int64) error {
	r.Lock()
	defer r.Unlock()

	query, err := reportsQueries.Pick(r.Type(), CmdDeleteReportsByMessage)
	if err != nil {
		return fmt.Errorf("failed to get query: %w", err)
	}
	query = r.Adopt(query)

	if _, err := r.ExecContext(ctx, query, r.GID(), msgID, chatID); err != nil {
		return fmt.Errorf("failed to delete reports: %w", err)
	}

	log.Printf("[INFO] all reports deleted for msgID:%d, chatID:%d", msgID, chatID)
	return nil
}

// cleanupOldReports removes ALL reports older than 7 days.
// retention period: 7 days - includes reports that:
//   - never reached threshold (notification_sent = false)
//   - reached threshold but notification update failed (notification_sent = true but orphaned)
//   - admin hasn't acted on within retention period
func (r *Reports) cleanupOldReports(ctx context.Context) error {
	// no lock - called from Add which already has lock
	const retentionDays = 7
	// removed notification_sent filter - delete ALL old reports to prevent memory leak
	query := r.Adopt("DELETE FROM reports WHERE gid = ? AND report_time < ?")
	cutoffTime := time.Now().Add(-retentionDays * 24 * time.Hour)

	result, err := r.ExecContext(ctx, query, r.GID(), cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to cleanup old reports: %w", err)
	}

	if rowsAffected, err := result.RowsAffected(); err == nil && rowsAffected > 0 {
		log.Printf("[DEBUG] cleaned up %d old reports (retention: %d days)", rowsAffected, retentionDays)
	}

	return nil
}

package storage

import (
	"context"
	"fmt"
	"time"
)

func (s *StorageTestSuite) TestReports_NewReports() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			s.Run("create new table", func() {
				_, err := NewReports(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE reports")

				// check if the table exists
				var exists int
				err = db.Get(&exists, `SELECT COUNT(*) FROM reports`)
				s.Require().NoError(err)
				s.Equal(0, exists) // empty but exists
			})

			s.Run("nil db connection", func() {
				_, err := NewReports(ctx, nil)
				s.Require().Error(err)
				s.Contains(err.Error(), "db connection is nil")
			})

			s.Run("context cancelled", func() {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				_, err := NewReports(ctx, db)
				s.Require().Error(err)
				defer db.Exec("DROP TABLE reports")
			})
		})
	}
}

func (s *StorageTestSuite) TestReports_Add() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			reports, err := NewReports(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE reports")

			s.Run("add single report", func() {
				report := Report{
					MsgID:            123,
					ChatID:           456,
					ReporterUserID:   789,
					ReporterUserName: "reporter1",
					ReportedUserID:   999,
					ReportedUserName: "spammer",
					MsgText:          "spam message",
				}

				err := reports.Add(ctx, report)
				s.Require().NoError(err)

				// verify report was added
				allReports, err := reports.GetByMessage(ctx, 123, 456)
				s.Require().NoError(err)
				s.Equal(1, len(allReports))
				s.Equal(int64(789), allReports[0].ReporterUserID)
				s.Equal("reporter1", allReports[0].ReporterUserName)
				s.Equal(int64(999), allReports[0].ReportedUserID)
				s.Equal("spammer", allReports[0].ReportedUserName)
				s.Equal("spam message", allReports[0].MsgText)
				s.False(allReports[0].NotificationSent)
				s.Equal(0, allReports[0].AdminMsgID)
			})

			s.Run("add multiple reports for same message", func() {
				report1 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 1, ReporterUserName: "user1", ReportedUserID: 999, ReportedUserName: "spammer", MsgText: "spam"}
				report2 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 2, ReporterUserName: "user2", ReportedUserID: 999, ReportedUserName: "spammer", MsgText: "spam"}
				report3 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 3, ReporterUserName: "user3", ReportedUserID: 999, ReportedUserName: "spammer", MsgText: "spam"}

				err := reports.Add(ctx, report1)
				s.Require().NoError(err)
				err = reports.Add(ctx, report2)
				s.Require().NoError(err)
				err = reports.Add(ctx, report3)
				s.Require().NoError(err)

				allReports, err := reports.GetByMessage(ctx, 100, 200)
				s.Require().NoError(err)
				s.Equal(3, len(allReports))
			})

			s.Run("duplicate report ignored", func() {
				report := Report{MsgID: 111, ChatID: 222, ReporterUserID: 333, ReporterUserName: "user", ReportedUserID: 444, ReportedUserName: "spammer", MsgText: "spam"}

				err := reports.Add(ctx, report)
				s.Require().NoError(err)

				// try to add same report again
				err = reports.Add(ctx, report)
				s.Require().NoError(err) // should not error, just ignore

				allReports, err := reports.GetByMessage(ctx, 111, 222)
				s.Require().NoError(err)
				s.Equal(1, len(allReports)) // still only one report
			})
		})
	}
}

func (s *StorageTestSuite) TestReports_GetByMessage() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			reports, err := NewReports(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE reports")

			s.Run("get reports for non-existent message", func() {
				allReports, err := reports.GetByMessage(ctx, 999, 888)
				s.Require().NoError(err)
				s.Equal(0, len(allReports))
			})

			s.Run("get reports ordered by time", func() {
				report1 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 1, ReporterUserName: "user1", ReportedUserID: 999, ReportedUserName: "spammer", MsgText: "spam"}
				time.Sleep(10 * time.Millisecond)
				report2 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 2, ReporterUserName: "user2", ReportedUserID: 999, ReportedUserName: "spammer", MsgText: "spam"}
				time.Sleep(10 * time.Millisecond)
				report3 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 3, ReporterUserName: "user3", ReportedUserID: 999, ReportedUserName: "spammer", MsgText: "spam"}

				err := reports.Add(ctx, report1)
				s.Require().NoError(err)
				err = reports.Add(ctx, report2)
				s.Require().NoError(err)
				err = reports.Add(ctx, report3)
				s.Require().NoError(err)

				allReports, err := reports.GetByMessage(ctx, 100, 200)
				s.Require().NoError(err)
				s.Require().Equal(3, len(allReports))

				// verify ordering (ascending by time)
				s.Equal(int64(1), allReports[0].ReporterUserID)
				s.Equal(int64(2), allReports[1].ReporterUserID)
				s.Equal(int64(3), allReports[2].ReporterUserID)
			})
		})
	}
}

func (s *StorageTestSuite) TestReports_GetReporterCountSince() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			reports, err := NewReports(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE reports")

			s.Run("count reports from single user", func() {
				report1 := Report{MsgID: 1, ChatID: 100, ReporterUserID: 555, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "msg"}
				report2 := Report{MsgID: 2, ChatID: 100, ReporterUserID: 555, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "msg"}
				report3 := Report{MsgID: 3, ChatID: 100, ReporterUserID: 555, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "msg"}

				err := reports.Add(ctx, report1)
				s.Require().NoError(err)
				err = reports.Add(ctx, report2)
				s.Require().NoError(err)
				err = reports.Add(ctx, report3)
				s.Require().NoError(err)

				count, err := reports.GetReporterCountSince(ctx, 555, time.Now().Add(-time.Hour))
				s.Require().NoError(err)
				s.Equal(3, count)
			})

			s.Run("count only recent reports", func() {
				// add old report
				oldReport := Report{MsgID: 10, ChatID: 100, ReporterUserID: 666, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "old", ReportTime: time.Now().Add(-2 * time.Hour)}
				err := reports.Add(ctx, oldReport)
				s.Require().NoError(err)

				// add recent report
				newReport := Report{MsgID: 11, ChatID: 100, ReporterUserID: 666, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "new"}
				err = reports.Add(ctx, newReport)
				s.Require().NoError(err)

				// count only reports in last hour
				count, err := reports.GetReporterCountSince(ctx, 666, time.Now().Add(-time.Hour))
				s.Require().NoError(err)
				s.Equal(1, count) // only the recent one
			})

			s.Run("count zero for user with no reports", func() {
				count, err := reports.GetReporterCountSince(ctx, 777, time.Now().Add(-time.Hour))
				s.Require().NoError(err)
				s.Equal(0, count)
			})
		})
	}
}

func (s *StorageTestSuite) TestReports_UpdateAdminMsgID() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			reports, err := NewReports(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE reports")

			s.Run("update admin msg id for all reports", func() {
				report1 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 1, ReporterUserName: "user1", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "spam"}
				report2 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 2, ReporterUserName: "user2", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "spam"}

				err := reports.Add(ctx, report1)
				s.Require().NoError(err)
				err = reports.Add(ctx, report2)
				s.Require().NoError(err)

				// update admin message ID
				err = reports.UpdateAdminMsgID(ctx, 100, 200, 12345)
				s.Require().NoError(err)

				// verify all reports for this message have admin_msg_id set
				allReports, err := reports.GetByMessage(ctx, 100, 200)
				s.Require().NoError(err)
				s.Require().Equal(2, len(allReports))
				for _, r := range allReports {
					s.Equal(12345, r.AdminMsgID)
					s.True(r.NotificationSent)
				}
			})

			s.Run("update does not affect other messages", func() {
				report1 := Report{MsgID: 111, ChatID: 200, ReporterUserID: 1, ReporterUserName: "user1", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "spam"}
				report2 := Report{MsgID: 222, ChatID: 200, ReporterUserID: 2, ReporterUserName: "user2", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "spam"}

				err := reports.Add(ctx, report1)
				s.Require().NoError(err)
				err = reports.Add(ctx, report2)
				s.Require().NoError(err)

				// update only first message
				err = reports.UpdateAdminMsgID(ctx, 111, 200, 99999)
				s.Require().NoError(err)

				// verify only first message updated
				report1List, err := reports.GetByMessage(ctx, 111, 200)
				s.Require().NoError(err)
				s.Equal(99999, report1List[0].AdminMsgID)
				s.True(report1List[0].NotificationSent)

				report2List, err := reports.GetByMessage(ctx, 222, 200)
				s.Require().NoError(err)
				s.Equal(0, report2List[0].AdminMsgID)
				s.False(report2List[0].NotificationSent)
			})
		})
	}
}

func (s *StorageTestSuite) TestReports_DeleteReporter() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			reports, err := NewReports(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE reports")

			s.Run("delete specific reporter", func() {
				report1 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 1, ReporterUserName: "user1", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "spam"}
				report2 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 2, ReporterUserName: "user2", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "spam"}
				report3 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 3, ReporterUserName: "user3", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "spam"}

				err := reports.Add(ctx, report1)
				s.Require().NoError(err)
				err = reports.Add(ctx, report2)
				s.Require().NoError(err)
				err = reports.Add(ctx, report3)
				s.Require().NoError(err)

				// delete reporter 2
				err = reports.DeleteReporter(ctx, 2, 100, 200)
				s.Require().NoError(err)

				// verify only 2 reports remain
				allReports, err := reports.GetByMessage(ctx, 100, 200)
				s.Require().NoError(err)
				s.Equal(2, len(allReports))
				s.Equal(int64(1), allReports[0].ReporterUserID)
				s.Equal(int64(3), allReports[1].ReporterUserID)
			})
		})
	}
}

func (s *StorageTestSuite) TestReports_DeleteByMessage() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			reports, err := NewReports(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE reports")

			s.Run("delete all reports for message", func() {
				report1 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 1, ReporterUserName: "user1", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "spam"}
				report2 := Report{MsgID: 100, ChatID: 200, ReporterUserID: 2, ReporterUserName: "user2", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "spam"}
				report3 := Report{MsgID: 200, ChatID: 200, ReporterUserID: 3, ReporterUserName: "user3", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "other"}

				err := reports.Add(ctx, report1)
				s.Require().NoError(err)
				err = reports.Add(ctx, report2)
				s.Require().NoError(err)
				err = reports.Add(ctx, report3)
				s.Require().NoError(err)

				// delete all reports for message 100
				err = reports.DeleteByMessage(ctx, 100, 200)
				s.Require().NoError(err)

				// verify message 100 has no reports
				msg100Reports, err := reports.GetByMessage(ctx, 100, 200)
				s.Require().NoError(err)
				s.Equal(0, len(msg100Reports))

				// verify message 200 still has report
				msg200Reports, err := reports.GetByMessage(ctx, 200, 200)
				s.Require().NoError(err)
				s.Equal(1, len(msg200Reports))
			})
		})
	}
}

func (s *StorageTestSuite) TestReports_CleanupOldReports() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			reports, err := NewReports(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE reports")

			s.Run("cleanup old reports without notification", func() {
				// add old report without notification
				oldReport := Report{MsgID: 100, ChatID: 200, ReporterUserID: 1, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "old", ReportTime: time.Now().Add(-8 * 24 * time.Hour)}
				err := reports.Add(ctx, oldReport)
				s.Require().NoError(err)

				// add recent report
				recentReport := Report{MsgID: 200, ChatID: 200, ReporterUserID: 2, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "recent"}
				err = reports.Add(ctx, recentReport)
				s.Require().NoError(err)

				// trigger cleanup (happens automatically in Add, but let's verify)
				allReports100, err := reports.GetByMessage(ctx, 100, 200)
				s.Require().NoError(err)
				s.Equal(0, len(allReports100)) // old report should be cleaned up

				allReports200, err := reports.GetByMessage(ctx, 200, 200)
				s.Require().NoError(err)
				s.Equal(1, len(allReports200)) // recent report should remain
			})

			s.Run("cleanup old reports regardless of notification status", func() {
				// add report (will be recent when added)
				oldReport := Report{MsgID: 300, ChatID: 200, ReporterUserID: 1, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "old"}
				err := reports.Add(ctx, oldReport)
				s.Require().NoError(err)

				// mark as notified immediately
				err = reports.UpdateAdminMsgID(ctx, 300, 200, 12345)
				s.Require().NoError(err)

				// manually update the report_time to be old (simulate aging)
				// this is done directly in DB to bypass the Add method which would trigger cleanup
				query := reports.Adopt("UPDATE reports SET report_time = ? WHERE msg_id = ? AND chat_id = ? AND gid = ?")
				_, err = reports.ExecContext(ctx, query, time.Now().Add(-8*24*time.Hour), 300, 200, reports.GID())
				s.Require().NoError(err)

				// add another report to trigger cleanup
				triggerReport := Report{MsgID: 400, ChatID: 200, ReporterUserID: 2, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "trigger"}
				err = reports.Add(ctx, triggerReport)
				s.Require().NoError(err)

				// verify old notified report was cleaned up (new behavior: all old reports deleted regardless of notification status)
				allReports, err := reports.GetByMessage(ctx, 300, 200)
				s.Require().NoError(err)
				s.Equal(0, len(allReports)) // should be cleaned up even though notification was sent
			})

			s.Run("keep recent reports with notification", func() {
				// add recent report with notification
				recentReport := Report{MsgID: 500, ChatID: 200, ReporterUserID: 1, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "recent"}
				err := reports.Add(ctx, recentReport)
				s.Require().NoError(err)

				// mark as notified
				err = reports.UpdateAdminMsgID(ctx, 500, 200, 54321)
				s.Require().NoError(err)

				// add trigger report to run cleanup
				triggerReport := Report{MsgID: 600, ChatID: 200, ReporterUserID: 2, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "trigger"}
				err = reports.Add(ctx, triggerReport)
				s.Require().NoError(err)

				// verify recent notified report still exists (not old enough to clean)
				allReports, err := reports.GetByMessage(ctx, 500, 200)
				s.Require().NoError(err)
				s.Equal(1, len(allReports)) // should not be cleaned up because it's recent
			})

			s.Run("boundary condition: exactly 7 days old", func() {
				// add report that will be exactly 7 days old
				boundaryReport := Report{MsgID: 700, ChatID: 200, ReporterUserID: 1, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "boundary"}
				err := reports.Add(ctx, boundaryReport)
				s.Require().NoError(err)

				// manually update the report_time to be exactly 7 days old (just at the edge)
				query := reports.Adopt("UPDATE reports SET report_time = ? WHERE msg_id = ? AND chat_id = ? AND gid = ?")
				exactlySevenDays := time.Now().Add(-7 * 24 * time.Hour)
				_, err = reports.ExecContext(ctx, query, exactlySevenDays, 700, 200, reports.GID())
				s.Require().NoError(err)

				// add trigger report to run cleanup
				triggerReport := Report{MsgID: 800, ChatID: 200, ReporterUserID: 2, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "trigger"}
				err = reports.Add(ctx, triggerReport)
				s.Require().NoError(err)

				// verify boundary report was cleaned up (>= 7 days means it should be deleted)
				allReports, err := reports.GetByMessage(ctx, 700, 200)
				s.Require().NoError(err)
				s.Equal(0, len(allReports)) // should be cleaned up at exactly 7 days
			})

			s.Run("cleanup called from Add method", func() {
				// this test verifies cleanup happens automatically when Add is called
				// add old report
				oldReport := Report{MsgID: 900, ChatID: 200, ReporterUserID: 1, ReporterUserName: "user", ReportedUserID: 999, ReportedUserName: "spam", MsgText: "old", ReportTime: time.Now().Add(-8 * 24 * time.Hour)}
				err := reports.Add(ctx, oldReport)
				s.Require().NoError(err)

				// old report should already be cleaned by the Add call itself
				allReports, err := reports.GetByMessage(ctx, 900, 200)
				s.Require().NoError(err)
				s.Equal(0, len(allReports)) // cleanup happens in Add, so old report already gone
			})
		})
	}
}

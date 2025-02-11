package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

func (s *StorageTestSuite) TestSampleUpdater() {
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {

			s.Run("append and read with normal timeout", func() {
				defer db.Exec("DROP TABLE IF EXISTS samples")
				samples, err := NewSamples(context.Background(), db)
				s.Require().NoError(err)

				updater := NewSampleUpdater(samples, SampleTypeSpam, time.Second)
				s.Require().NoError(updater.Append("test spam message"))

				reader, err := updater.Reader()
				s.Require().NoError(err)

				data, err := io.ReadAll(reader)
				s.Require().NoError(err)
				s.Assert().Equal("test spam message\n", string(data))
			})

			s.Run("append multiple messages", func() {
				defer db.Exec("DROP TABLE IF EXISTS samples")
				samples, err := NewSamples(context.Background(), db)
				s.Require().NoError(err)

				updater := NewSampleUpdater(samples, SampleTypeHam, 1*time.Second)

				messages := []string{"msg1", "msg2", "msg3"}
				for _, msg := range messages {
					s.Require().NoError(updater.Append(msg))
					time.Sleep(time.Millisecond) // ensure messages have different timestamps
				}

				reader, err := updater.Reader()
				s.Require().NoError(err)
				defer reader.Close()

				data, err := io.ReadAll(reader)
				s.Require().NoError(err)
				result := strings.Split(strings.TrimSpace(string(data)), "\n")
				s.Assert().Equal(len(messages), len(result))
				for _, msg := range messages {
					s.Assert().Contains(result, msg)
				}
			})

			s.Run("timeout on append", func() {
				defer db.Exec("DROP TABLE IF EXISTS samples")
				samples, err := NewSamples(context.Background(), db)
				s.Require().NoError(err)

				updater := NewSampleUpdater(samples, SampleTypeSpam, time.Nanosecond)
				time.Sleep(time.Microsecond)
				err = updater.Append("timeout message")
				s.Require().Error(err)
				s.Contains(err.Error(), "context deadline exceeded")
			})

			s.Run("tiny timeout", func() {
				defer db.Exec("DROP TABLE IF EXISTS samples")
				samples, err := NewSamples(context.Background(), db)
				s.Require().NoError(err)

				updater := NewSampleUpdater(samples, SampleTypeSpam, 1)
				s.Assert().Error(updater.Append("test message"))
			})

			s.Run("verify user origin", func() {
				defer db.Exec("DROP TABLE IF EXISTS samples")
				samples, err := NewSamples(context.Background(), db)
				s.Require().NoError(err)

				updater := NewSampleUpdater(samples, SampleTypeSpam, time.Second)
				s.Require().NoError(updater.Append("test message"))

				// verify the message was stored with user origin
				ctx := context.Background()
				messages, err := samples.Read(ctx, SampleTypeSpam, SampleOriginUser)
				s.Require().NoError(err)
				s.Assert().Contains(messages, "test message")

				// verify it's not in preset origin
				messages, err = samples.Read(ctx, SampleTypeSpam, SampleOriginPreset)
				s.Require().NoError(err)
				s.Assert().NotContains(messages, "test message")
			})

			s.Run("sample type consistency", func() {
				defer db.Exec("DROP TABLE IF EXISTS samples")
				samples, err := NewSamples(context.Background(), db)
				s.Require().NoError(err)

				spamUpdater := NewSampleUpdater(samples, SampleTypeSpam, time.Second)
				hamUpdater := NewSampleUpdater(samples, SampleTypeHam, time.Second)

				s.Require().NoError(spamUpdater.Append("spam message"))
				s.Require().NoError(hamUpdater.Append("ham message"))

				ctx := context.Background()

				// verify spam messages
				messages, err := samples.Read(ctx, SampleTypeSpam, SampleOriginUser)
				s.Require().NoError(err)
				s.Assert().Contains(messages, "spam message")
				s.Assert().NotContains(messages, "ham message")

				// verify ham messages
				messages, err = samples.Read(ctx, SampleTypeHam, SampleOriginUser)
				s.Require().NoError(err)
				s.Assert().Contains(messages, "ham message")
				s.Assert().NotContains(messages, "spam message")
			})

			s.Run("read empty", func() {
				defer db.Exec("DROP TABLE IF EXISTS samples")
				samples, err := NewSamples(context.Background(), db)
				s.Require().NoError(err)

				updater := NewSampleUpdater(samples, SampleTypeSpam, time.Second)
				reader, err := updater.Reader()
				s.Require().NoError(err)
				defer reader.Close()

				data, err := reader.Read(make([]byte, 100))
				s.Require().Equal(0, data)
				s.Require().Equal(io.EOF, err)
			})

			s.Run("timeout triggers", func() {
				defer db.Exec("DROP TABLE IF EXISTS samples")
				samples, err := NewSamples(context.Background(), db)
				s.Require().NoError(err)

				updater := NewSampleUpdater(samples, SampleTypeSpam, time.Nanosecond)
				time.Sleep(time.Microsecond)
				s.Require().Error(updater.Append("test"))
			})

			s.Run("no timeout", func() {
				defer db.Exec("DROP TABLE IF EXISTS samples")
				samples, err := NewSamples(context.Background(), db)
				s.Require().NoError(err)

				updater := NewSampleUpdater(samples, SampleTypeSpam, 0)
				s.Require().NoError(updater.Append("test"))

				reader, err := updater.Reader()
				s.Require().NoError(err)
				defer reader.Close()

				data := make([]byte, 100)
				n, err := reader.Read(data)
				s.Require().NoError(err)
				s.Assert().Equal("test\n", string(data[:n]))
			})

			s.Run("remove message", func() {
				defer db.Exec("DROP TABLE IF EXISTS samples")
				samples, err := NewSamples(context.Background(), db)
				s.Require().NoError(err)

				updater := NewSampleUpdater(samples, SampleTypeSpam, time.Second)
				s.Require().NoError(updater.Append("test message"))
				s.Require().NoError(updater.Remove("test message"))

				reader, err := updater.Reader()
				s.Require().NoError(err)
				defer reader.Close()

				data, err := io.ReadAll(reader)
				s.Require().NoError(err)
				s.Assert().Empty(string(data))
			})

			s.Run("remove with timeout", func() {
				defer db.Exec("DROP TABLE IF EXISTS samples")
				samples, err := NewSamples(context.Background(), db)
				s.Require().NoError(err)

				updater := NewSampleUpdater(samples, SampleTypeSpam, time.Nanosecond)
				time.Sleep(time.Microsecond)
				err = updater.Remove("test message")
				s.Require().Error(err)
				s.Assert().Contains(err.Error(), "context deadline exceeded")
			})

			s.Run("remove non-existent", func() {
				defer db.Exec("DROP TABLE IF EXISTS samples")
				samples, err := NewSamples(context.Background(), db)
				s.Require().NoError(err)

				updater := NewSampleUpdater(samples, SampleTypeSpam, time.Second)
				err = updater.Remove("non-existent message")
				s.Require().Error(err)
			})
		})
	}

}

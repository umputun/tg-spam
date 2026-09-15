package storage

import (
	"context"
	"io"
	"time"
)

// SampleUpdater is an adaptor on top of Samples store, to update dynamic (user's) samples for detector, either ham or spam.
// The service is used by the detector to update the samples.
type SampleUpdater struct {
	samplesService *Samples
	sampleType     SampleType
	timeout        time.Duration
}

// NewSampleUpdater creates a new SampleUpdater instance with the given samples service, sample type and timeout.
func NewSampleUpdater(samplesService *Samples, sampleType SampleType, timeout time.Duration) *SampleUpdater {
	return &SampleUpdater{samplesService: samplesService, sampleType: sampleType, timeout: timeout}
}

// Append a message to the samples, forcing user origin
func (u *SampleUpdater) Append(msg string) error {
	ctx, cancel := context.Background(), func() {}
	if u.timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), u.timeout)
	}
	defer cancel()
	return u.samplesService.Add(ctx, u.sampleType, SampleOriginUser, msg)
}

// Remove a message from the samples
func (u *SampleUpdater) Remove(msg string) error {
	ctx, cancel := context.Background(), func() {}
	if u.timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), u.timeout)
	}
	defer cancel()
	return u.samplesService.DeleteMessage(ctx, msg)
}

// Reader returns a reader for the samples
func (u *SampleUpdater) Reader() (io.ReadCloser, error) {
	// we don't want to pass context with timeout here, as it's an async operation
	return u.samplesService.Reader(context.Background(), u.sampleType, SampleOriginUser)
}

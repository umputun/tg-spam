package imagerecognizer

import (
	"context"

	"github.com/umputun/tg-spam/lib/tgspam"
)

func NewNoOpImageRecognizer() tgspam.ImageRecognizer {
	return noOpImageRecognizer{}
}

type noOpImageRecognizer struct{}

func (noOpImageRecognizer) RecognizeImage(ctx context.Context, request tgspam.RecognizeImageRequest) (*tgspam.RecognizeImageResponse, error) {
	return &tgspam.RecognizeImageResponse{}, nil
}

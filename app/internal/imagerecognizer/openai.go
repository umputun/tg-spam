package imagerecognizer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/umputun/tg-spam/lib/tgspam"
)

type OpenAiImageRecognizerSettings struct {
	TelegramBotToken            string
	OpenAiApiKey                string
	OpenAiImageRecognitionModel string

	// OptionalTelegramApiHost is useful for testing purposes.
	OptionalTelegramApiHost string `json:",omitempty"`

	// OptionalOpenAiApiHost is useful for testing purposes.
	OptionalOpenAiApiHost string `json:",omitempty"`
}

func (settings *OpenAiImageRecognizerSettings) validate() error {
	var aErr error

	if settings.TelegramBotToken == "" {
		aErr = errors.Join(aErr, fmt.Errorf("telegram bot token is required"))
	}

	if settings.OpenAiApiKey == "" {
		aErr = errors.Join(aErr, fmt.Errorf("OpenAI api key is required"))
	}

	if settings.OpenAiImageRecognitionModel == "" {
		aErr = errors.Join(aErr, fmt.Errorf("OpenAI image recognition model is required"))
	}

	return aErr
}

func (settings OpenAiImageRecognizerSettings) Print() string {
	settings.TelegramBotToken = "****"
	settings.OpenAiApiKey = "****"

	jsonView, _ := json.Marshal(settings)

	return string(jsonView)
}

func NewOpenAiImageRecognizer(settings OpenAiImageRecognizerSettings) (tgspam.ImageRecognizer, error) {
	err := settings.validate()
	if err != nil {
		return nil, fmt.Errorf("validating settings: %w", err)
	}

	var recognizer openAiImageRecognizer

	recognizer.telegramApiHost = "https://api.telegram.org"
	if settings.OptionalTelegramApiHost != "" {
		recognizer.telegramApiHost = settings.OptionalTelegramApiHost
	}

	recognizer.telegramBotToken = settings.TelegramBotToken

	recognizer.openAiApiHost = "https://api.openai.com"
	if settings.OptionalOpenAiApiHost != "" {
		recognizer.openAiApiHost = settings.OptionalOpenAiApiHost
	}

	recognizer.openAiImageRecognitionModel = settings.OpenAiImageRecognitionModel

	recognizer.openAiApiKey = settings.OpenAiApiKey

	return &recognizer, nil
}

type openAiImageRecognizer struct {
	telegramApiHost  string
	telegramBotToken string

	openAiApiHost               string
	openAiImageRecognitionModel string
	openAiApiKey                string
}

func (o *openAiImageRecognizer) RecognizeImage(ctx context.Context, request tgspam.RecognizeImageRequest) (*tgspam.RecognizeImageResponse, error) {
	imageContent, err := o.getImageTelegramFileContent(ctx, request.ImageTelegramFileID)
	if err != nil {
		return nil, fmt.Errorf("getting image telegram file content: %w", err)
	}

	openAiRecognizeImageReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.openAiApiHost+"/v1/responses", bytes.NewReader([]byte(`
{
  "model": "`+o.openAiImageRecognitionModel+`",
  "input": [
    {
      "role": "system",
      "content": [
        {
          "type": "input_text",
          "text": "You are an optical character recognition system helping users extract text from images."
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "input_image",
          "detail": "high",
          "image_url": "data:image/jpeg;base64,`+base64.StdEncoding.EncodeToString(imageContent)+`"
        }
      ]
    }
  ],
  "text": {
    "format": {
      "type": "json_schema",
      "strict": true,
      "name": "image_ocr_extraction_v1",
      "schema": {
        "type": "object",
        "properties": {
          "recognized_text": {
            "type": "string",
            "description": "The full text recognized from the image. Use the same language as in the image. If no text is found, return an empty string."
          }
        },
        "required": [
          "recognized_text"
        ],
        "additionalProperties": false
      }
    }
  }
}
`)))
	if err != nil {
		return nil, fmt.Errorf("creating OpenAI image recognition request: %w", err)
	}

	openAiRecognizeImageReq.Header.Set("Content-Type", "application/json")
	openAiRecognizeImageReq.Header.Set("Authorization", "Bearer "+o.openAiApiKey)

	openAiRecognizeImageRes, err := http.DefaultClient.Do(openAiRecognizeImageReq)
	if err != nil {
		return nil, fmt.Errorf("requesting OpenAI image recognition: %w", err)
	}

	openAiRecognizeImageResBody, err := io.ReadAll(openAiRecognizeImageRes.Body)
	if err != nil {
		return nil, fmt.Errorf("reading OpenAI image recognition response body: %w", err)
	}

	log.Printf("[DEBUG] OpenAI image recognition response body: %s", openAiRecognizeImageResBody)

	if openAiRecognizeImageRes.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenAI image recognition request failed with status %d: %s", openAiRecognizeImageRes.StatusCode, string(openAiRecognizeImageResBody))
	}

	type openAiRecognizeImageResponseDataModel struct {
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}

	var openAiRecognizeImageResponseData openAiRecognizeImageResponseDataModel
	err = json.Unmarshal(openAiRecognizeImageResBody, &openAiRecognizeImageResponseData)
	if err != nil {
		return nil, fmt.Errorf("decoding OpenAI image recognition response: %w", err)
	}

	if len(openAiRecognizeImageResponseData.Output) != 0 && len(openAiRecognizeImageResponseData.Output[0].Content) != 0 {
		type customJsonOpenAiRecognizeImageSchema struct {
			RecognizedText string `json:"recognized_text"`
		}

		var recognizedImageData customJsonOpenAiRecognizeImageSchema
		err = json.Unmarshal([]byte(openAiRecognizeImageResponseData.Output[0].Content[0].Text), &recognizedImageData)
		if err != nil {
			return nil, fmt.Errorf("decoding recognized image data: %w", err)
		}

		return &tgspam.RecognizeImageResponse{
			RecognizedText: recognizedImageData.RecognizedText,
		}, nil
	}

	return nil, errors.New("invalid OpenAI image recognition response: no output content found")
}

func (o *openAiImageRecognizer) getImageTelegramFileContent(ctx context.Context, imageTelegramFileID string) ([]byte, error) {
	telegramAPIHost := o.telegramApiHost
	token := o.telegramBotToken
	fileID := imageTelegramFileID

	fileMetaReq, err := http.NewRequestWithContext(ctx, http.MethodGet, telegramAPIHost+"/bot"+token+"/getFile?file_id="+fileID, nil)
	if err != nil {
		return nil, fmt.Errorf("creating telegram file metadata request: %w", err)
	}

	fileMetaRes, err := http.DefaultClient.Do(fileMetaReq)
	if err != nil {
		return nil, fmt.Errorf("requesting telegram file metadata: %w", err)
	}
	defer fileMetaRes.Body.Close()

	fileMetaResBody, err := io.ReadAll(fileMetaRes.Body)
	if err != nil {
		return nil, fmt.Errorf("reading telegram file metadata response body: %w", err)
	}

	log.Printf("[DEBUG] telegram file metadata response body: %s", fileMetaResBody)

	if fileMetaRes.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram file metadata request failed with status %d: %s", fileMetaRes.StatusCode, string(fileMetaResBody))
	}

	type fileMetaResModel struct {
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}

	var fileMetaData fileMetaResModel
	err = json.Unmarshal(fileMetaResBody, &fileMetaData)
	if err != nil {
		return nil, fmt.Errorf("decoding telegram file metadata response: %w", err)
	}

	fileContentReq, err := http.NewRequestWithContext(ctx, http.MethodGet, telegramAPIHost+"/file/bot"+token+"/"+fileMetaData.Result.FilePath, nil)
	if err != nil {
		return nil, fmt.Errorf("creating telegram file content request: %w", err)
	}

	fileContentRes, err := http.DefaultClient.Do(fileContentReq)
	if err != nil {
		return nil, fmt.Errorf("requesting telegram file content: %w", err)
	}
	defer fileContentRes.Body.Close()

	fileContentResBody, err := io.ReadAll(fileContentRes.Body)
	if err != nil {
		return nil, fmt.Errorf("reading telegram file content response body: %w", err)
	}

	if fileContentRes.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram file content request failed with status %d: %s", fileContentRes.StatusCode, string(fileContentResBody))
	}

	return fileContentResBody, nil
}

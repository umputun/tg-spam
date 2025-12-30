package imagerecognizer_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/internal/imagerecognizer"
	"github.com/umputun/tg-spam/lib/tgspam"
)

func TestOpenAiImageRecognizer(t *testing.T) {
	t.Parallel()

	t.Run("settings is validated", func(t *testing.T) {
		t.Parallel()

		validSettings := func() imagerecognizer.OpenAiImageRecognizerSettings {
			var settings imagerecognizer.OpenAiImageRecognizerSettings
			settings.TelegramBotToken = "not empty"
			settings.OpenAiApiKey = "not empty"
			settings.OpenAiImageRecognitionModel = "not empty"

			return settings
		}

		for condition, params := range map[string]struct {
			getSettings func() imagerecognizer.OpenAiImageRecognizerSettings
			expectErr   bool
		}{
			"valid": {
				getSettings: validSettings,
				expectErr:   false,
			},
			"telegram bot token is required": {
				getSettings: func() imagerecognizer.OpenAiImageRecognizerSettings {
					s := validSettings()
					s.TelegramBotToken = ""
					return s
				},
				expectErr: true,
			},
			"OpenAI api key is required": {
				getSettings: func() imagerecognizer.OpenAiImageRecognizerSettings {
					s := validSettings()
					s.OpenAiApiKey = ""
					return s
				},
				expectErr: true,
			},
			"OpenAI image recognition model is required": {
				getSettings: func() imagerecognizer.OpenAiImageRecognizerSettings {
					s := validSettings()
					s.OpenAiImageRecognitionModel = ""
					return s
				},
				expectErr: true,
			},
		} {
			t.Run(condition, func(t *testing.T) {
				t.Parallel()

				_, err := imagerecognizer.NewOpenAiImageRecognizer(params.getSettings())
				if params.expectErr {
					assert.Error(t, err, "should be an error")
				} else {
					assert.NoError(t, err, "should not be an error")
				}
			})
		}
	})

	t.Run("settings print masks sensitive data", func(t *testing.T) {
		t.Parallel()

		settings := imagerecognizer.OpenAiImageRecognizerSettings{
			TelegramBotToken:            "secret",
			OpenAiApiKey:                "secret",
			OpenAiImageRecognitionModel: "OpenAI image recognition model",
			OptionalTelegramApiHost:     "telegram api host",
			OptionalOpenAiApiHost:       "OpenAI api host",
		}

		printedSettings := settings.Print()

		assert.NotContains(t, printedSettings, "secret", "should mask sensitive data")
		assert.Contains(t, printedSettings, "OpenAI image recognition model", "no OpenAI image recognition model")
		assert.Contains(t, printedSettings, "telegram api host", "no telegram api host")
		assert.Contains(t, printedSettings, "OpenAI api host", "no OpenAI api host")
	})

	t.Run("it recognizes image", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		fakeTelegramApi := httptest.NewServer(func() http.Handler {
			mux := http.NewServeMux()

			mux.HandleFunc("GET /{botToken}/getFile", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`
{
  "ok": true,
  "result": {
    "file_id": "some file id",
    "file_unique_id": "some unique id",
    "file_size": 163846,
    "file_path": "photos/file_1"
  }
}`))
			})

			mux.HandleFunc("GET /file/{botToken}/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = w.Write([]byte("fake image content"))
			})

			return mux
		}())
		t.Cleanup(fakeTelegramApi.Close)

		fakeOpenAiApi := httptest.NewServer(func() http.Handler {
			mux := http.NewServeMux()

			mux.HandleFunc("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`
{
  "id": "some response id",
  "object": "response",
  "created_at": 1766966180,
  "status": "completed",
  "background": false,
  "billing": {
    "payer": "developer"
  },
  "completed_at": 1766966182,
  "error": null,
  "incomplete_details": null,
  "instructions": null,
  "max_output_tokens": null,
  "max_tool_calls": null,
  "model": "gpt-4o-mini-2024-07-18",
  "output": [
    {
      "id": "some message id",
      "type": "message",
      "status": "completed",
      "content": [
        {
          "type": "output_text",
          "annotations": [],
          "logprobs": [],
          "text": "{\"recognized_text\":\"\u0421\u0422\u0410\u0420\u0422\\n\u0411\u0415\u0417 \u041e\u041f\u042b\u0422\u0410\\n\u0414\u043e\u0445\u043e\u0434 \u043e\u0442 5 500 \u20bd\\n\u043a\u0430\u0436\u0434\u044b\u0439 \u0434\u0435\u043d\u044c\\n\u0422\u043e\u043b\u044c\u043a\u043e \u0434\u043b\u044f\\n\u0441\u043e\u0432\u0435\u0440\u0448\u0435\u043d\u043d\u043e\u043b\u0435\u0442\u043d\u0438\u0445\\n\u041e\u0431\u044a\u044f\u0441\u043d\u044e \u0432\u0441\u0451 \u043f\u043e\u0448\u0430\u0433\u043e\u0432\u043e\\n\u041d\u0430\u043f\u0438\u0448\u0438 \u2193\"}"
        }
      ],
      "role": "assistant"
    }
  ],
  "parallel_tool_calls": true,
  "previous_response_id": null,
  "prompt_cache_key": null,
  "prompt_cache_retention": null,
  "reasoning": {
    "effort": null,
    "summary": null
  },
  "safety_identifier": null,
  "service_tier": "default",
  "store": true,
  "temperature": 1.0,
  "text": {
    "format": {
      "type": "json_schema",
      "description": null,
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
      },
      "strict": true
    },
    "verbosity": "medium"
  },
  "tool_choice": "auto",
  "tools": [],
  "top_logprobs": 0,
  "top_p": 1.0,
  "truncation": "disabled",
  "usage": {
    "input_tokens": 36920,
    "input_tokens_details": {
      "cached_tokens": 0
    },
    "output_tokens": 52,
    "output_tokens_details": {
      "reasoning_tokens": 0
    },
    "total_tokens": 36972
  },
  "user": null,
  "metadata": {}
}`))
			})

			return mux
		}())
		t.Cleanup(fakeOpenAiApi.Close)

		settings := imagerecognizer.OpenAiImageRecognizerSettings{
			TelegramBotToken:            "secret",
			OpenAiApiKey:                "secret",
			OpenAiImageRecognitionModel: "OpenAI image recognition model",
			OptionalTelegramApiHost:     fakeTelegramApi.URL,
			OptionalOpenAiApiHost:       fakeOpenAiApi.URL,
		}

		recognizer, err := imagerecognizer.NewOpenAiImageRecognizer(settings)
		require.NoError(t, err, "creating OpenAI image recognizer")

		var request tgspam.RecognizeImageRequest
		request.ImageTelegramFileID = "test-image-file-id"

		response, err := recognizer.RecognizeImage(ctx, request)
		require.NoError(t, err, "recognizing image")

		assert.Equal(t, "СТАРТ\nБЕЗ ОПЫТА\nДоход от 5 500 ₽\nкаждый день\nТолько для\nсовершеннолетних\nОбъясню всё пошагово\nНапиши ↓", response.RecognizedText, "wrong recognized text")
	})
}

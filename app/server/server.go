package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth_chi"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-pkgz/lgr"
	"github.com/go-pkgz/rest"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

//go:generate moq --out mocks/tb_api.go --pkg mocks --with-resets --skip-ensure . TbAPI
//go:generate moq --out mocks/detector.go --pkg mocks --with-resets --skip-ensure . Detector

// SpamWeb is a web server for ban/unban actions
type SpamWeb struct {
	Config
	TbAPI    TbAPI
	detector Detector
	chatID   int64

	unbanned struct {
		sync.RWMutex
		users map[int64]time.Time
	}
}

// Config defines web server parameters
type Config struct {
	Version    string // version to show in /ping
	Secret     string // secret key to sign url tokens
	URL        string // root url
	ListenAddr string // listen address
	TgGroup    string // telegram group name/id
	MaxMsg     int    // max message size
}

// TbAPI is an interface for telegram bot API, only a subset of all methods used
type TbAPI interface {
	Send(c tbapi.Chattable) (tbapi.Message, error)
	Request(c tbapi.Chattable) (*tbapi.APIResponse, error)
	GetChat(config tbapi.ChatInfoConfig) (tbapi.Chat, error)
}

// Detector is a spam detector interface, only a subset of all methods used for ham updates
type Detector interface {
	UpdateHam(msg string) error
}

// NewSpamWeb creates new server
func NewSpamWeb(tbAPI TbAPI, detector Detector, params Config) (*SpamWeb, error) {
	res := SpamWeb{Config: params, TbAPI: tbAPI, detector: detector}
	res.unbanned.users = make(map[int64]time.Time)
	chatID, err := res.getChatID(params.TgGroup)
	if err != nil {
		return nil, fmt.Errorf("can't get chat ID for %s: %w", params.TgGroup, err)
	}
	res.chatID = chatID
	return &res, nil
}

// Run starts server and accepts requests to unban users from telegram
func (s *SpamWeb) Run(ctx context.Context) error {
	router := chi.NewRouter()
	router.Use(rest.Recoverer(lgr.Default()))
	router.Use(middleware.Throttle(1000), middleware.Timeout(60*time.Second))
	router.Use(rest.AppInfo("tg-spam", "umputun", s.Version), rest.Ping)
	router.Use(tollbooth_chi.LimitHandler(tollbooth.NewLimiter(10, nil)))

	router.Get("/unban", s.unbanHandler)

	srv := &http.Server{Addr: s.ListenAddr, Handler: router, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("[WARN] failed to shutdown unban server: %v", err)
		}
	}()

	log.Printf("[INFO] start server on %s", s.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to run server: %w", err)
	}
	return nil
}

type htmlResponse struct {
	Title      string
	Message    string
	Background string
	Foreground string
	StatusCode int
}

// UnbanHandler handles unban requests, GET /unban?user=<user_id>&token=<token>&msg=message
func (s *SpamWeb) unbanHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("user")
	token := r.URL.Query().Get("token")
	userID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		log.Printf("[WARN] failed to get user ID for %q, %v", id, err)
		resp := htmlResponse{
			Title:      "Error",
			Message:    fmt.Sprintf("failed to get user ID for %q: %v", id, err),
			Background: "#ff6347",
			Foreground: "#ffffff",
			StatusCode: http.StatusBadRequest,
		}
		s.sendHTML(w, resp)
		return
	}
	expToken := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d::%s", userID, s.Secret))))
	if len(token) != len(expToken) || subtle.ConstantTimeCompare([]byte(token), []byte(expToken)) != 1 {
		log.Printf("[WARN] invalid token for %q", id)
		resp := htmlResponse{
			Title:      "Error",
			Message:    fmt.Sprintf("invalid token for %q", id),
			Background: "#ff6347",
			Foreground: "#ffffff",
			StatusCode: http.StatusForbidden,
		}
		s.sendHTML(w, resp)
		return
	}

	// check if user is already unbanned
	isAlreadyUnbanned, tsPrevUnban := func() (bool, time.Time) {
		s.unbanned.RLock()
		defer s.unbanned.RUnlock()
		ts, ok := s.unbanned.users[userID]
		return ok, ts
	}()

	if isAlreadyUnbanned {
		log.Printf("[WARN] user %d already unbanned ", userID)
		resp := htmlResponse{
			Title:      "Error",
			Message:    fmt.Sprintf("user %d already unbanned %v ago", userID, time.Since(tsPrevUnban).Round(time.Second)),
			Background: "#ff6347",
			Foreground: "#ffffff",
			StatusCode: http.StatusBadRequest,
		}
		s.sendHTML(w, resp)
		return
	}

	if comprMsg := r.URL.Query().Get("msg"); comprMsg != "" {
		msg, derr := s.decompressString(comprMsg)
		if derr != nil {
			log.Printf("[WARN] failed to decompress message %q, %v", comprMsg, derr)
		} else {
			if derr := s.detector.UpdateHam(msg); derr != nil {
				log.Printf("[WARN] failed to update ham, %v", derr)
			}
		}
	}

	log.Printf("[INFO] unban user %d", userID)
	_, err = s.TbAPI.Request(tbapi.UnbanChatMemberConfig{ChatMemberConfig: tbapi.ChatMemberConfig{UserID: userID, ChatID: s.chatID}})
	if err != nil {
		log.Printf("[WARN] failed to unban %s, %v", id, err)
		resp := htmlResponse{
			Title:      "Error",
			Message:    fmt.Sprintf("failed to unban %s: %v", id, err),
			Background: "#ff6347",
			Foreground: "#ffffff",
			StatusCode: http.StatusInternalServerError,
		}
		s.sendHTML(w, resp)
		return
	}

	resp := htmlResponse{
		Title:      "Success",
		Message:    fmt.Sprintf("user %d unbanned", userID),
		Background: "#90ee90",
		Foreground: "#000000",
		StatusCode: http.StatusOK,
	}

	s.unbanned.Lock()
	s.unbanned.users[userID] = time.Now()
	s.unbanned.Unlock()

	s.sendHTML(w, resp)
}

func (s *SpamWeb) getChatID(group string) (int64, error) {
	chatID, err := strconv.ParseInt(group, 10, 64)
	if err == nil {
		return chatID, nil
	}

	chat, err := s.TbAPI.GetChat(tbapi.ChatInfoConfig{ChatConfig: tbapi.ChatConfig{SuperGroupUsername: "@" + group}})
	if err != nil {
		return 0, fmt.Errorf("can't get chat for %s: %w", group, err)
	}

	return chat.ID, nil
}

// UnbanURL returns URL to unban user
func (s *SpamWeb) UnbanURL(userID int64, msg string) string {
	// key is SHA1 of user ID + secret
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d::%s", userID, s.Secret))))
	res := fmt.Sprintf("%s/unban?user=%d&token=%s", s.URL, userID, key)
	if msg != "" {
		if cs, err := s.compressString(msg, s.MaxMsg); err == nil {
			res += fmt.Sprintf("&msg=%s", cs)
		} else {
			log.Printf("[WARN] failed to compress message %q, %v", msg, err)
		}
	}
	return res
}

func (s *SpamWeb) sendHTML(w http.ResponseWriter, resp htmlResponse) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(resp.StatusCode)

	htmlTmpl := template.Must(template.New("msg").Parse(msgTemplate))
	if err := htmlTmpl.Execute(w, resp); err != nil {
		log.Printf("[WARN] failed to execute template, %v", err)
		return
	}
}

const compressedPrefix = "1-"

// compressString compresses the input string, encodes it in base64 for URL safety, and checks if the
// compressed size is within the specified maximum limit. If compression is ineffective (resulting in a larger string),
// it returns the base64-encoded original string instead.
func (s *SpamWeb) compressString(input string, max int) (string, error) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	if _, err := gz.Write([]byte(input)); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}

	compressed := compressedPrefix + base64.URLEncoding.EncodeToString(b.Bytes())
	encodedOriginal := base64.URLEncoding.EncodeToString([]byte(input))

	out := compressed
	if len(encodedOriginal) < len(compressed) {
		out = encodedOriginal
	}

	if len(out) >= max {
		return "", fmt.Errorf("encoded string is too long: %d characters", len(out))
	}

	return out, nil
}

// decompressString checks if the input string is prefixed with a specific flag indicating compression.
// If so, it decompresses the string; otherwise, it decodes it from base64. This function is designed to
// work in tandem with CompressString, handling both compressed and uncompressed (but encoded) strings.
func (s *SpamWeb) decompressString(compressed string) (string, error) {
	if strings.HasPrefix(compressed, compressedPrefix) {
		trimmedInput := strings.TrimPrefix(compressed, compressedPrefix)
		data, err := base64.URLEncoding.DecodeString(trimmedInput)
		if err != nil {
			return "", err
		}

		var b bytes.Buffer
		r, err := gzip.NewReader(bytes.NewBuffer(data))
		if err != nil {
			return "", err
		}
		defer r.Close()

		limR := &io.LimitedReader{R: r, N: 102400} // Limit the amount of data that can be read to 100k.
		if _, err := io.Copy(&b, limR); err != nil {
			return "", err
		}

		return b.String(), nil
	}

	// Handle as base64 encoded if the prefix is not present
	decoded, err := base64.URLEncoding.DecodeString(compressed)
	if err != nil {
		return "", err
	}

	return string(decoded), nil
}

var msgTemplate = `<!DOCTYPE html>
<html>
<head>
    <title>{{.Title}}</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            height: 100vh;
            margin: 0;
        }
        .center-block {
            width: 60%;
            padding: 20px;
            text-align: center;
            border-radius: 10px;
            background-color: {{.Background}};
            color: {{.Foreground}};
        }
    </style>
</head>
<body>
    <div class="center-block">
        {{.Message}}
    </div>
</body>
</html>`

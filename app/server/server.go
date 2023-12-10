package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
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

// SpamWeb is a web server for ban/unban actions
type SpamWeb struct {
	Config
	TbAPI  TbAPI
	chatID int64
}

// Config defines web server parameters
type Config struct {
	Version    string // version to show in /ping
	Secret     string // secret key to sign url tokens
	URL        string // root url
	ListenAddr string // listen address
	TgGroup    string // telegram group name/id
}

// TbAPI is an interface for telegram bot API, only a subset of all methods used
type TbAPI interface {
	Send(c tbapi.Chattable) (tbapi.Message, error)
	Request(c tbapi.Chattable) (*tbapi.APIResponse, error)
	GetChat(config tbapi.ChatInfoConfig) (tbapi.Chat, error)
}

// NewSpamWeb creates new server
func NewSpamWeb(tbAPI TbAPI, params Config) (*SpamWeb, error) {
	res := SpamWeb{Config: params, TbAPI: tbAPI}
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
	router.Use(rest.Recoverer(lgr.Default()), middleware.GetHead)
	router.Use(middleware.Throttle(1000), middleware.Timeout(60*time.Second))
	router.Use(rest.AppInfo("tg-spam", "umputun", s.Version), rest.Ping)
	router.Use(tollbooth_chi.LimitHandler(tollbooth.NewLimiter(5, nil)))

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

// UnbanHandler handles unban requests, GET /unban?user=<user_id>&token=<token>
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
func (s *SpamWeb) UnbanURL(userID int64) string {
	// key is SHA1 of user ID + secret
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d::%s", userID, s.Secret))))
	return fmt.Sprintf("%s/unban?user=%d&token=%s", s.URL, userID, key)
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

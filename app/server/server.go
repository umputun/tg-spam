package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

//go:generate moq --out mocks/tb_api.go --pkg mocks --with-resets --skip-ensure . TbAPI

// SpamRest is a REST API for ban/unban actions
type SpamRest struct {
	Params
	TbAPI  TbAPI
	chatID int64
}

type Params struct {
	Secret     string // secret key to sign url tokens
	URL        string // root url
	ListenAddr string // listen address
	TgGroup    string // telegram group name/id
}

// TbAPI is an interface for telegram bot API, only subset of methods used
type TbAPI interface {
	Send(c tbapi.Chattable) (tbapi.Message, error)
	Request(c tbapi.Chattable) (*tbapi.APIResponse, error)
	GetChat(config tbapi.ChatInfoConfig) (tbapi.Chat, error)
}

// NewSpamRest creates new REST API server
func NewSpamRest(tbAPI TbAPI, params Params) (*SpamRest, error) {
	res := SpamRest{Params: params, TbAPI: tbAPI}
	chatID, err := res.getChatID(params.TgGroup)
	if err != nil {
		return nil, fmt.Errorf("can't get chat ID for %s: %w", params.TgGroup, err)
	}
	res.chatID = chatID
	return &res, nil
}

// Run starts REST API server
func (s *SpamRest) Run(ctx context.Context) error {

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Application", "tg-spam")
		if _, err := w.Write([]byte("pong")); err != nil {
			log.Printf("[WARN] failed to write response, %v", err)
		}
	})
	mux.HandleFunc("/unban", s.unbanHandler)

	srv := &http.Server{Addr: s.ListenAddr, Handler: mux, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}

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

// UnbanHandler handles unban requests, GET /unban?user=<user_id>&token=<token>
func (s *SpamRest) unbanHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("user")
	token := r.URL.Query().Get("token")
	userID, err := s.getChatID(id)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "failed to get user ID for %q: %v", id, err)
		return
	}
	expToken := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d::%s", userID, s.Secret))))
	if len(token) != len(expToken) || subtle.ConstantTimeCompare([]byte(token), []byte(expToken)) != 1 {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	log.Printf("[INFO] unban user %s (%d)", id, userID)
	_, err = s.TbAPI.Request(tbapi.UnbanChatMemberConfig{ChatMemberConfig: tbapi.ChatMemberConfig{UserID: userID, ChatID: s.chatID}})
	if err != nil {
		log.Printf("[WARN] failed to unban %s, %v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "failed to unban %s: %v", id, err)
		return
	}
	if _, err := w.Write([]byte("ok")); err != nil {
		log.Printf("[WARN] failed to write response, %v", err)
	}
}

func (s *SpamRest) getChatID(group string) (int64, error) {
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
func (s *SpamRest) UnbanURL(userID int64) string {
	// key is SHA1 of user ID + secret
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d::%s", userID, s.Secret))))
	return fmt.Sprintf("%s/unban?user=%d&token=%s", s.URL, userID, key)
}

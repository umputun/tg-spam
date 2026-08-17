// Package tgbotapi has functions and types used for interacting with
// the Telegram Bot API.
package tgbotapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// HTTPClient is the type needed for the bot to perform HTTP requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// BotAPI allows you to interact with the Telegram Bot API.
type BotAPI struct {
	Token  string `json:"token"`
	Debug  bool   `json:"debug"`
	Buffer int    `json:"buffer"`

	Self   User       `json:"-"`
	Client HTTPClient `json:"-"`

	apiEndpoint     string
	fileEndpoint    string
	logger          any
	loggingDisabled bool

	stoppers []context.CancelFunc
	mu       sync.RWMutex
}

// NewBotAPI creates a new BotAPI instance.
//
// It requires a token, provided by @BotFather on Telegram.
func NewBotAPI(token string) (*BotAPI, error) {
	return NewBotAPIWithOptions(token)
}

// NewBotAPIWithAPIEndpoint creates a new BotAPI instance
// and allows you to pass API endpoint.
//
// It requires a token, provided by @BotFather on Telegram and API endpoint.
//
// Deprecated: Use [NewBotAPIWithOptions] with [WithAPIEndpoint] instead.
func NewBotAPIWithAPIEndpoint(token, apiEndpoint string) (*BotAPI, error) {
	return NewBotAPIWithOptions(token, WithAPIEndpoint(apiEndpoint))
}

// NewBotAPIWithClient creates a new BotAPI instance
// and allows you to pass a http.Client.
//
// It requires a token, provided by @BotFather on Telegram and API endpoint.
//
// Deprecated: Use [NewBotAPIWithOptions] with [WithAPIEndpoint] and [WithHTTPClient] instead.
func NewBotAPIWithClient(token, apiEndpoint string, client HTTPClient) (*BotAPI, error) {
	return NewBotAPIWithOptions(token, WithAPIEndpoint(apiEndpoint), WithHTTPClient(client))
}

// NewBotAPIWithOptions creates a new BotAPI instance using optional configuration.
//
// It requires a token, provided by @BotFather on Telegram.
func NewBotAPIWithOptions(token string, options ...BotAPIOption) (*BotAPI, error) {
	config := defaultBotAPIConfig()
	for _, option := range options {
		if err := option(&config); err != nil {
			return nil, err
		}
	}

	bot := &BotAPI{
		Token:           token,
		Debug:           config.debug,
		Buffer:          config.buffer,
		Client:          config.client,
		apiEndpoint:     config.apiEndpoint,
		fileEndpoint:    config.fileEndpoint,
		logger:          config.logger,
		loggingDisabled: config.loggingDisabled,
	}

	self, err := bot.GetMe()
	if err != nil {
		return nil, err
	}

	bot.Self = self

	return bot, nil
}

// SetAPIEndpoint changes the Telegram Bot API endpoint used by the instance.
func (bot *BotAPI) SetAPIEndpoint(apiEndpoint string) {
	bot.apiEndpoint = apiEndpoint
}

// SetFileEndpoint changes the Telegram file download endpoint used by the instance.
func (bot *BotAPI) SetFileEndpoint(fileEndpoint string) {
	bot.fileEndpoint = fileEndpoint
}

// SetUpdatesBuffer changes the Telegram Bot API update chan buffer used by the instance.
func (bot *BotAPI) SetUpdatesBuffer(capacity int) {
	bot.Buffer = capacity
}

func buildParams(in Params) url.Values {
	if in == nil {
		return url.Values{}
	}

	out := url.Values{}

	for key, value := range in {
		out.Set(key, value)
	}

	return out
}

// MakeRequest makes a request to a specific endpoint with our token.
func (bot *BotAPI) MakeRequest(endpoint string, params Params) (*APIResponse, error) {
	return bot.MakeRequestWithContext(context.Background(), endpoint, params)
}

func (bot *BotAPI) MakeRequestWithContext(ctx context.Context, endpoint string, params Params) (*APIResponse, error) {
	return bot.executeRequest(ctx, endpoint, buildFormPayload(params), requestDebug{params: params})
}

func (bot *BotAPI) executeRequest(ctx context.Context, endpoint string, payload requestPayload, debugInfo requestDebug) (*APIResponse, error) {
	defer payload.close()

	bot.logRequestDebug(ctx, endpoint, debugInfo)

	method := fmt.Sprintf(bot.apiEndpoint, bot.Token, endpoint)

	req, err := http.NewRequestWithContext(ctx, "POST", method, payload.body)
	if err != nil {
		return &APIResponse{}, err
	}
	if payload.contentType != "" {
		req.Header.Set("Content-Type", payload.contentType)
	}

	resp, err := bot.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp APIResponse
	bytes, err := bot.decodeAPIResponse(resp.Body, &apiResp)
	if err != nil {
		return &apiResp, err
	}

	bot.logResponseDebug(ctx, endpoint, string(bytes))

	if !apiResp.Ok {
		var parameters ResponseParameters

		if apiResp.Parameters != nil {
			parameters = *apiResp.Parameters
		}

		return &apiResp, &Error{
			Code:               apiResp.ErrorCode,
			Message:            apiResp.Description,
			ResponseParameters: parameters,
		}
	}

	return &apiResp, nil
}

// decodeAPIResponse decode response and return slice of bytes if debug enabled.
// If debug disabled, just decode http.Response.Body stream to APIResponse struct
// for efficient memory usage
func (bot *BotAPI) decodeAPIResponse(responseBody io.Reader, resp *APIResponse) ([]byte, error) {
	if !bot.debugLoggingEnabled() {
		dec := json.NewDecoder(responseBody)
		err := dec.Decode(resp)
		return nil, err
	}

	// if debug, read response body
	data, err := io.ReadAll(responseBody)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(data, resp)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// UploadFiles makes a request to the API with files.
func (bot *BotAPI) UploadFiles(endpoint string, params Params, files []RequestFile) (*APIResponse, error) {
	return bot.UploadFilesWithContext(context.Background(), endpoint, params, files)
}

func (bot *BotAPI) UploadFilesWithContext(ctx context.Context, endpoint string, params Params, files []RequestFile) (*APIResponse, error) {
	payload, err := buildMultipartPayload(params, files)
	if err != nil {
		return nil, err
	}

	return bot.executeRequest(ctx, endpoint, payload, requestDebug{
		params:    params,
		fileCount: len(files),
	})
}

// GetFileDirectURL returns direct URL to file
//
// It requires the FileID.
func (bot *BotAPI) GetFileDirectURL(fileID string) (string, error) {
	file, err := bot.GetFile(FileConfig{fileID})
	if err != nil {
		return "", err
	}

	return bot.FileURL(file), nil
}

// FileURL returns a full path to the download URL for a File using this bot's file endpoint.
func (bot *BotAPI) FileURL(file File) string {
	return fmt.Sprintf(bot.fileEndpoint, bot.Token, file.FilePath)
}

// GetMe fetches the currently authenticated bot.
//
// This method is called upon creation to validate the token,
// and so you may get this data from BotAPI.Self without the need for
// another request.
func (bot *BotAPI) GetMe() (User, error) {
	return bot.GetMeWithContext(context.Background())
}

func (bot *BotAPI) GetMeWithContext(ctx context.Context) (User, error) {
	resp, err := bot.MakeRequestWithContext(ctx, "getMe", nil)
	if err != nil {
		return User{}, err
	}

	var user User
	err = json.Unmarshal(resp.Result, &user)

	return user, err
}

// IsMessageToMe returns true if message directed to this bot.
//
// It requires the Message.
func (bot *BotAPI) IsMessageToMe(message Message) bool {
	return strings.Contains(message.Text, "@"+bot.Self.UserName)
}

// Request sends a Chattable to Telegram, and returns the APIResponse.
func (bot *BotAPI) Request(c Chattable) (*APIResponse, error) {
	return bot.RequestWithContext(context.Background(), c)
}

func (bot *BotAPI) RequestWithContext(ctx context.Context, c Chattable) (*APIResponse, error) {
	params, err := c.params()
	if err != nil {
		return nil, err
	}

	if t, ok := c.(Fileable); ok {
		plan := uploadPlanFromFiles(t.files())
		params = plan.Apply(params)

		if plan.NeedsUpload() {
			return bot.UploadFilesWithContext(ctx, t.method(), params, plan.Files())
		}
	}

	return bot.MakeRequestWithContext(ctx, c.method(), params)
}

// Send will send a Chattable item to Telegram and provides the
// returned Message.
func (bot *BotAPI) Send(c Chattable) (Message, error) {
	resp, err := bot.Request(c)
	if err != nil {
		return Message{}, err
	}

	var message Message
	err = json.Unmarshal(resp.Result, &message)

	return message, err
}

func (bot *BotAPI) requestBool(c Chattable) (bool, error) {
	resp, err := bot.Request(c)
	if err != nil {
		return false, err
	}

	var ok bool
	err = json.Unmarshal(resp.Result, &ok)

	return ok, err
}

// SendLivePhoto sends a live photo and returns the resulting message.
func (bot *BotAPI) SendLivePhoto(config SendLivePhotoConfig) (Message, error) {
	return bot.Send(config)
}

// SendRichMessage sends a rich message and returns the resulting message.
func (bot *BotAPI) SendRichMessage(config SendRichMessageConfig) (Message, error) {
	return bot.Send(config)
}

// SendRichMessageDraft streams a partial rich message draft.
func (bot *BotAPI) SendRichMessageDraft(config SendRichMessageDraftConfig) (bool, error) {
	return bot.requestBool(config)
}

// EditEphemeralMessageText edits an ephemeral text message.
func (bot *BotAPI) EditEphemeralMessageText(config EditEphemeralMessageTextConfig) (bool, error) {
	return bot.requestBool(config)
}

// EditEphemeralMessageMedia edits the media of an ephemeral message.
func (bot *BotAPI) EditEphemeralMessageMedia(config EditEphemeralMessageMediaConfig) (bool, error) {
	return bot.requestBool(config)
}

// EditEphemeralMessageCaption edits the caption of an ephemeral message.
func (bot *BotAPI) EditEphemeralMessageCaption(config EditEphemeralMessageCaptionConfig) (bool, error) {
	return bot.requestBool(config)
}

// EditEphemeralMessageReplyMarkup edits the reply markup of an ephemeral message.
func (bot *BotAPI) EditEphemeralMessageReplyMarkup(config EditEphemeralMessageReplyMarkupConfig) (bool, error) {
	return bot.requestBool(config)
}

// DeleteEphemeralMessage deletes an ephemeral message.
func (bot *BotAPI) DeleteEphemeralMessage(config DeleteEphemeralMessageConfig) (bool, error) {
	return bot.requestBool(config)
}

// SendMediaGroup sends a media group and returns the resulting messages.
func (bot *BotAPI) SendMediaGroup(config MediaGroupConfig) ([]Message, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return nil, err
	}

	var messages []Message
	err = json.Unmarshal(resp.Result, &messages)

	return messages, err
}

// PostStory posts a story on behalf of a managed business account.
func (bot *BotAPI) PostStory(config PostStoryConfig) (Story, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return Story{}, err
	}

	var story Story
	err = json.Unmarshal(resp.Result, &story)

	return story, err
}

// EditStory edits a story posted by a managed business account.
func (bot *BotAPI) EditStory(config EditStoryConfig) (Story, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return Story{}, err
	}

	var story Story
	err = json.Unmarshal(resp.Result, &story)

	return story, err
}

// RepostStory reposts a story to a managed business account.
func (bot *BotAPI) RepostStory(config RepostStoryConfig) (Story, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return Story{}, err
	}

	var story Story
	err = json.Unmarshal(resp.Result, &story)

	return story, err
}

// GetUserProfilePhotos gets a user's profile photos.
//
// It requires UserID.
// Offset and Limit are optional.
func (bot *BotAPI) GetUserProfilePhotos(config UserProfilePhotosConfig) (UserProfilePhotos, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return UserProfilePhotos{}, err
	}

	var profilePhotos UserProfilePhotos
	err = json.Unmarshal(resp.Result, &profilePhotos)

	return profilePhotos, err
}

// GetUserProfileAudios gets a user's profile audios.
func (bot *BotAPI) GetUserProfileAudios(config UserProfileAudiosConfig) (UserProfileAudios, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return UserProfileAudios{}, err
	}

	var profileAudios UserProfileAudios
	err = json.Unmarshal(resp.Result, &profileAudios)

	return profileAudios, err
}

// GetUserPersonalChatMessages gets recent messages from the channel pinned to a user's profile.
func (bot *BotAPI) GetUserPersonalChatMessages(config UserPersonalChatMessagesConfig) ([]Message, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return nil, err
	}

	var messages []Message
	err = json.Unmarshal(resp.Result, &messages)

	return messages, err
}

// GetFile returns a File which can download a file from Telegram.
//
// Requires FileID.
func (bot *BotAPI) GetFile(config FileConfig) (File, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return File{}, err
	}

	var file File
	err = json.Unmarshal(resp.Result, &file)

	return file, err
}

// GetUpdates fetches updates.
// If a WebHook is set, this will not return any data!
//
// Offset, Limit, Timeout, and AllowedUpdates are optional.
// To avoid stale items, set Offset to one higher than the previous item.
// Set Timeout to a large number to reduce requests, so you can get updates
// instantly instead of having to wait between requests.
func (bot *BotAPI) GetUpdates(config UpdateConfig) ([]Update, error) {
	return bot.GetUpdatesWithContext(context.Background(), config)
}

func (bot *BotAPI) GetUpdatesWithContext(ctx context.Context, config UpdateConfig) ([]Update, error) {
	resp, err := bot.RequestWithContext(ctx, config)
	if err != nil {
		return []Update{}, err
	}

	var updates []Update
	err = json.Unmarshal(resp.Result, &updates)

	return updates, err
}

// GetWebhookInfo allows you to fetch information about a webhook and if
// one currently is set, along with pending update count and error messages.
func (bot *BotAPI) GetWebhookInfo() (WebhookInfo, error) {
	return bot.GetWebhookInfoWithContext(context.Background())
}

func (bot *BotAPI) GetWebhookInfoWithContext(ctx context.Context) (WebhookInfo, error) {
	resp, err := bot.MakeRequestWithContext(ctx, "getWebhookInfo", nil)
	if err != nil {
		return WebhookInfo{}, err
	}

	var info WebhookInfo
	err = json.Unmarshal(resp.Result, &info)

	return info, err
}

// GetUpdatesChan starts and returns a channel for getting updates.
func (bot *BotAPI) GetUpdatesChan(config UpdateConfig) UpdatesChannel {
	ch := make(chan Update, bot.Buffer)

	ctx, cancel := context.WithCancel(context.Background())
	bot.mu.Lock()
	bot.stoppers = append(bot.stoppers, cancel)
	bot.mu.Unlock()

	go func() {
		for {
			select {
			case <-ctx.Done():
				close(ch)
				return
			default:
			}

			updates, err := bot.GetUpdatesWithContext(ctx, config)
			if err != nil {
				if ctx.Err() == nil {
					bot.logUpdateError(ctx, err)
					time.Sleep(time.Second * 3)
				}
				continue
			}

			for _, update := range updates {
				if update.UpdateID >= config.Offset {
					config.Offset = update.UpdateID + 1
					ch <- update
				}
			}
		}
	}()

	return ch
}

// StopReceivingUpdates stops the go routine which receives updates
func (bot *BotAPI) StopReceivingUpdates() {
	bot.mu.Lock()
	defer bot.mu.Unlock()

	bot.logDebug(context.Background(), "Stopping the update receiver routine...")
	for _, stopper := range bot.stoppers {
		stopper()
	}
}

// ListenForWebhook registers a http handler for a webhook.
func (bot *BotAPI) ListenForWebhook(pattern string) UpdatesChannel {
	ch := make(chan Update, bot.Buffer)

	http.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		update, err := bot.HandleUpdate(r)
		if err != nil {
			errMsg, _ := json.Marshal(map[string]string{"error": err.Error()})
			w.WriteHeader(http.StatusBadRequest)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(errMsg)
			return
		}

		ch <- *update
	})

	return ch
}

// ListenForWebhookRespReqFormat registers a http handler for a single incoming webhook.
func (bot *BotAPI) ListenForWebhookRespReqFormat(w http.ResponseWriter, r *http.Request) UpdatesChannel {
	ch := make(chan Update, bot.Buffer)

	func(w http.ResponseWriter, r *http.Request) {
		defer close(ch)

		update, err := bot.HandleUpdate(r)
		if err != nil {
			errMsg, _ := json.Marshal(map[string]string{"error": err.Error()})
			w.WriteHeader(http.StatusBadRequest)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(errMsg)
			return
		}

		ch <- *update
	}(w, r)

	return ch
}

// HandleUpdate parses and returns update received via webhook
func (bot *BotAPI) HandleUpdate(r *http.Request) (*Update, error) {
	if r.Method != http.MethodPost {
		err := errors.New("wrong HTTP method required POST")
		return nil, err
	}

	var update Update
	err := json.NewDecoder(r.Body).Decode(&update)
	if err != nil {
		return nil, err
	}

	return &update, nil
}

// WriteToHTTPResponse writes the request to the HTTP ResponseWriter.
//
// It doesn't support uploading files.
//
// See https://core.telegram.org/bots/api#making-requests-when-getting-updates
// for details.
func WriteToHTTPResponse(w http.ResponseWriter, c Chattable) error {
	params, err := c.params()
	if err != nil {
		return err
	}

	if t, ok := c.(Fileable); ok {
		plan := uploadPlanFromFiles(t.files())
		if plan.NeedsUpload() {
			return errors.New("unable to use http response to upload files")
		}
		params = plan.Apply(params)
	}

	values := buildParams(params)
	values.Set("method", c.method())

	w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
	_, err = w.Write([]byte(values.Encode()))
	return err
}

// GetChat gets information about a chat.
func (bot *BotAPI) GetChat(config ChatInfoConfig) (ChatFullInfo, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return ChatFullInfo{}, err
	}

	var chat ChatFullInfo
	err = json.Unmarshal(resp.Result, &chat)

	return chat, err
}

// GetChatAdministrators gets a list of administrators in the chat.
//
// If none have been appointed, only the creator will be returned.
// Bots are not shown, even if they are an administrator.
func (bot *BotAPI) GetChatAdministrators(config ChatAdministratorsConfig) ([]ChatMember, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return []ChatMember{}, err
	}

	var members []ChatMember
	err = json.Unmarshal(resp.Result, &members)

	return members, err
}

// GetChatMemberCount gets the number of users in a chat.
func (bot *BotAPI) GetChatMemberCount(config ChatMemberCountConfig) (int, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return -1, err
	}

	var count int
	err = json.Unmarshal(resp.Result, &count)

	return count, err
}

// GetChatMembersCount gets the number of users in a chat.
func (bot *BotAPI) GetChatMembersCount(config ChatMemberCountConfig) (int, error) {
	return bot.GetChatMemberCount(config)
}

// GetChatMember gets a specific chat member.
func (bot *BotAPI) GetChatMember(config GetChatMemberConfig) (ChatMember, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return ChatMember{}, err
	}

	var member ChatMember
	err = json.Unmarshal(resp.Result, &member)

	return member, err
}

// DeleteMessageReaction removes a reaction from a message.
func (bot *BotAPI) DeleteMessageReaction(config DeleteMessageReactionConfig) (bool, error) {
	return bot.requestBool(config)
}

// DeleteAllMessageReactions removes all recent reactions from a user or actor chat.
func (bot *BotAPI) DeleteAllMessageReactions(config DeleteAllMessageReactionsConfig) (bool, error) {
	return bot.requestBool(config)
}

// GetGameHighScores allows you to get the high scores for a game.
func (bot *BotAPI) GetGameHighScores(config GetGameHighScoresConfig) ([]GameHighScore, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return []GameHighScore{}, err
	}

	var highScores []GameHighScore
	err = json.Unmarshal(resp.Result, &highScores)

	return highScores, err
}

// GetInviteLink get InviteLink for a chat
func (bot *BotAPI) GetInviteLink(config ChatInviteLinkConfig) (string, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return "", err
	}

	var inviteLink string
	err = json.Unmarshal(resp.Result, &inviteLink)

	return inviteLink, err
}

// GetManagedBotToken gets the token of a managed bot.
func (bot *BotAPI) GetManagedBotToken(config GetManagedBotTokenConfig) (string, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return "", err
	}

	var token string
	err = json.Unmarshal(resp.Result, &token)

	return token, err
}

// ReplaceManagedBotToken revokes the current token of a managed bot and returns a new one.
func (bot *BotAPI) ReplaceManagedBotToken(config ReplaceManagedBotTokenConfig) (string, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return "", err
	}

	var token string
	err = json.Unmarshal(resp.Result, &token)

	return token, err
}

// GetManagedBotAccessSettings gets granular access settings for a managed bot.
func (bot *BotAPI) GetManagedBotAccessSettings(config GetManagedBotAccessSettingsConfig) (BotAccessSettings, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return BotAccessSettings{}, err
	}

	var settings BotAccessSettings
	err = json.Unmarshal(resp.Result, &settings)

	return settings, err
}

// SetManagedBotAccessSettings changes granular access settings for a managed bot.
func (bot *BotAPI) SetManagedBotAccessSettings(config SetManagedBotAccessSettingsConfig) (bool, error) {
	return bot.requestBool(config)
}

// GetMyStarBalance gets the current Telegram Stars balance of the bot.
func (bot *BotAPI) GetMyStarBalance(config GetMyStarBalanceConfig) (StarAmount, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return StarAmount{}, err
	}

	var balance StarAmount
	err = json.Unmarshal(resp.Result, &balance)

	return balance, err
}

// GetBusinessAccountStarBalance gets the Telegram Stars balance of a business account.
func (bot *BotAPI) GetBusinessAccountStarBalance(config GetBusinessAccountStarBalanceConfig) (StarAmount, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return StarAmount{}, err
	}

	var balance StarAmount
	err = json.Unmarshal(resp.Result, &balance)

	return balance, err
}

// GetBusinessAccountGifts gets gifts owned by a business account.
func (bot *BotAPI) GetBusinessAccountGifts(config GetBusinessAccountGiftsConfig) (OwnedGifts, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return OwnedGifts{}, err
	}

	var gifts OwnedGifts
	err = json.Unmarshal(resp.Result, &gifts)

	return gifts, err
}

// GetUserGifts gets gifts owned by a user.
func (bot *BotAPI) GetUserGifts(config GetUserGiftsConfig) (OwnedGifts, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return OwnedGifts{}, err
	}

	var gifts OwnedGifts
	err = json.Unmarshal(resp.Result, &gifts)

	return gifts, err
}

// GetChatGifts gets gifts owned by a chat.
func (bot *BotAPI) GetChatGifts(config GetChatGiftsConfig) (OwnedGifts, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return OwnedGifts{}, err
	}

	var gifts OwnedGifts
	err = json.Unmarshal(resp.Result, &gifts)

	return gifts, err
}

// CreateInvoiceLink Use this method to create a link for an invoice. Returns the created invoice link as
// String on success.
func (bot *BotAPI) CreateInvoiceLink(config InvoiceLinkConfig) (inviteLink string, err error) {
	var resp *APIResponse

	if resp, err = bot.Request(config); err != nil {
		return
	}
	if !resp.Ok {
		err = fmt.Errorf("returns error code: %d", resp.ErrorCode)
		return
	}
	err = json.Unmarshal(resp.Result, &inviteLink)

	return
}

// GetStickerSet returns a StickerSet.
func (bot *BotAPI) GetStickerSet(config GetStickerSetConfig) (StickerSet, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return StickerSet{}, err
	}

	var stickerSet StickerSet
	err = json.Unmarshal(resp.Result, &stickerSet)

	return stickerSet, err
}

// GetCustomEmojiStickers returns a slice of Sticker objects.
func (bot *BotAPI) GetCustomEmojiStickers(config GetCustomEmojiStickersConfig) ([]Sticker, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return []Sticker{}, err
	}

	var stickers []Sticker
	err = json.Unmarshal(resp.Result, &stickers)

	return stickers, err
}

// StopPoll stops a poll and returns the result.
func (bot *BotAPI) StopPoll(config StopPollConfig) (Poll, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return Poll{}, err
	}

	var poll Poll
	err = json.Unmarshal(resp.Result, &poll)

	return poll, err
}

// GetMyCommands gets the currently registered commands.
func (bot *BotAPI) GetMyCommands() ([]BotCommand, error) {
	return bot.GetMyCommandsWithConfig(GetMyCommandsConfig{})
}

// GetMyCommandsWithConfig gets the currently registered commands with a config.
func (bot *BotAPI) GetMyCommandsWithConfig(config GetMyCommandsConfig) ([]BotCommand, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return nil, err
	}

	var commands []BotCommand
	err = json.Unmarshal(resp.Result, &commands)

	return commands, err
}

// CopyMessage copy messages of any kind. The method is analogous to the method
// forwardMessage, but the copied message doesn't have a link to the original
// message. Returns the MessageID of the sent message on success.
func (bot *BotAPI) CopyMessage(config CopyMessageConfig) (MessageID, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return MessageID{}, err
	}

	var messageID MessageID
	err = json.Unmarshal(resp.Result, &messageID)

	return messageID, err
}

// AnswerWebAppQuery sets the result of an interaction with a Web App and send a
// corresponding message on behalf of the user to the chat from which the query originated.
func (bot *BotAPI) AnswerWebAppQuery(config AnswerWebAppQueryConfig) (SentWebAppMessage, error) {
	var sentWebAppMessage SentWebAppMessage

	resp, err := bot.Request(config)
	if err != nil {
		return sentWebAppMessage, err
	}

	err = json.Unmarshal(resp.Result, &sentWebAppMessage)
	return sentWebAppMessage, err
}

// AnswerGuestQuery replies to a received guest message.
func (bot *BotAPI) AnswerGuestQuery(config AnswerGuestQueryConfig) (SentGuestMessage, error) {
	var sentGuestMessage SentGuestMessage

	resp, err := bot.Request(config)
	if err != nil {
		return sentGuestMessage, err
	}

	err = json.Unmarshal(resp.Result, &sentGuestMessage)
	return sentGuestMessage, err
}

// AnswerChatJoinRequestQuery processes a received chat join request query.
func (bot *BotAPI) AnswerChatJoinRequestQuery(config AnswerChatJoinRequestQueryConfig) (bool, error) {
	return bot.requestBool(config)
}

// SendChatJoinRequestWebApp processes a chat join request query by showing a Mini App.
func (bot *BotAPI) SendChatJoinRequestWebApp(config SendChatJoinRequestWebAppConfig) (bool, error) {
	return bot.requestBool(config)
}

// GetMyDefaultAdministratorRights gets the current default administrator rights of the bot.
func (bot *BotAPI) GetMyDefaultAdministratorRights(config GetMyDefaultAdministratorRightsConfig) (ChatAdministratorRights, error) {
	var rights ChatAdministratorRights

	resp, err := bot.Request(config)
	if err != nil {
		return rights, err
	}

	err = json.Unmarshal(resp.Result, &rights)
	return rights, err
}

// CreateForumTopic creates a topic in a forum supergroup chat.
func (bot *BotAPI) CreateForumTopic(config CreateForumTopicConfig) (ForumTopic, error) {
	var topic ForumTopic

	resp, err := bot.Request(config)
	if err != nil {
		return topic, err
	}

	err = json.Unmarshal(resp.Result, &topic)
	return topic, err
}

// SavePreparedInlineMessage Stores a message that can be sent by a user of a Mini App. Returns a PreparedInlineMessage object.
func SavePreparedInlineMessage[T InlineQueryResults](bot *BotAPI, config SavePreparedInlineMessageConfig[T]) (PreparedInlineMessage, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return PreparedInlineMessage{}, err
	}

	var preparedInlineMessage PreparedInlineMessage
	err = json.Unmarshal(resp.Result, &preparedInlineMessage)

	return preparedInlineMessage, err
}

// SavePreparedKeyboardButton stores a keyboard button that can be used by a user of a Mini App.
func (bot *BotAPI) SavePreparedKeyboardButton(config SavePreparedKeyboardButtonConfig) (PreparedKeyboardButton, error) {
	resp, err := bot.Request(config)
	if err != nil {
		return PreparedKeyboardButton{}, err
	}

	var preparedKeyboardButton PreparedKeyboardButton
	err = json.Unmarshal(resp.Result, &preparedKeyboardButton)

	return preparedKeyboardButton, err
}

// EscapeText takes an input text and escape Telegram markup symbols.
// In this way we can send a text without being afraid of having to escape the characters manually.
// Note that you don't have to include the formatting style in the input text, or it will be escaped too.
// If there is an error, an empty string will be returned.
//
// parseMode is the text formatting mode (ModeMarkdown, ModeMarkdownV2 or ModeHTML)
// text is the input string that will be escaped
func EscapeText(parseMode string, text string) string {
	var replacer *strings.Replacer

	if parseMode == ModeHTML {
		replacer = strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;")
	} else if parseMode == ModeMarkdown {
		replacer = strings.NewReplacer("_", "\\_", "*", "\\*", "`", "\\`", "[", "\\[")
	} else if parseMode == ModeMarkdownV2 {
		replacer = strings.NewReplacer(
			"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(",
			"\\(", ")", "\\)", "~", "\\~", "`", "\\`", ">", "\\>",
			"#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=", "|",
			"\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!",
		)
	} else {
		return ""
	}

	return replacer.Replace(text)
}

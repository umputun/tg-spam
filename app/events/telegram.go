package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/go-pkgz/notify"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/radio-t/super-bot/app/bot"
)

//go:generate moq --out mock_tb_api.go . tbAPI
//go:generate moq --out mock_msg_logger.go . msgLogger

// TelegramListener listens to tg update, forward to bots and send back responses
// Not thread safe
type TelegramListener struct {
	TbAPI                  tbAPI
	MsgLogger              msgLogger
	Bots                   bot.Interface
	Group                  string // can be int64 or public group username (without "@" prefix)
	Debug                  bool
	IdleDuration           time.Duration
	AllActivityTerm        Terminator // all activity for given user
	BotsActivityTerm       Terminator // bot-only activity for given user
	OverallBotActivityTerm Terminator // bot-only activity for all users
	SuperUsers             SuperUser
	chatID                 int64

	msgs struct {
		once sync.Once
		ch   chan bot.Response
	}
}

type tbAPI interface {
	GetUpdatesChan(config tbapi.UpdateConfig) tbapi.UpdatesChannel
	Send(c tbapi.Chattable) (tbapi.Message, error)
	Request(c tbapi.Chattable) (*tbapi.APIResponse, error)
	GetChat(config tbapi.ChatInfoConfig) (tbapi.Chat, error)
}

type msgLogger interface {
	Save(msg *bot.Message)
}

// Do process all events, blocked call
func (l *TelegramListener) Do(ctx context.Context) error {
	log.Printf("[INFO] start telegram listener for %q", l.Group)

	var getChatErr error
	if l.chatID, getChatErr = l.getChatID(l.Group); getChatErr != nil {
		return fmt.Errorf("failed to get chat ID for group %q: %w", l.Group, getChatErr)
	}

	l.msgs.once.Do(func() {
		l.msgs.ch = make(chan bot.Response, 100)
		if l.IdleDuration == 0 {
			l.IdleDuration = 30 * time.Second
		}
	})

	u := tbapi.NewUpdate(0)
	u.Timeout = 60

	updates := l.TbAPI.GetUpdatesChan(u)

	for {
		select {

		case <-ctx.Done():
			return ctx.Err()

		case update, ok := <-updates:
			if !ok {
				return fmt.Errorf("telegram update chan closed")
			}

			if update.Message == nil {
				log.Print("[DEBUG] empty message body")
				continue
			}

			msgJSON, errJSON := json.Marshal(update.Message)
			if errJSON != nil {
				log.Printf("[ERROR] failed to marshal update.Message to json: %v", errJSON)
				continue
			}
			log.Printf("[DEBUG] %s", string(msgJSON))

			if update.Message.Chat == nil {
				log.Print("[DEBUG] ignoring message not from chat")
				continue
			}

			fromChat := update.Message.Chat.ID

			msg := l.transform(update.Message)
			if fromChat == l.chatID {
				l.MsgLogger.Save(msg) // save an incoming update to report
			}

			log.Printf("[DEBUG] incoming msg: %+v", msg)

			// check for all-activity ban
			if b := l.AllActivityTerm.check(msg.From, msg.SenderChat, msg.Sent, fromChat); b.active {
				if b.new && !l.SuperUsers.IsSuper(update.Message.From.UserName) && fromChat == l.chatID {
					if err := l.applyBan(*msg, l.AllActivityTerm.BanDuration, fromChat, update.Message.From.ID); err != nil {
						log.Printf("[ERROR] can't ban for all activity, %v", err)
					}
				}
				continue
			}

			resp := l.Bots.OnMessage(*msg)

			if fromChat == l.chatID && l.botActivityBan(resp, *msg, fromChat, update.Message.From.ID) {
				log.Printf("[INFO] bot activity ban initiated for %+v", update.Message.From)
				continue
			}

			if err := l.sendBotResponse(resp, fromChat); err != nil {
				log.Printf("[WARN] failed to respond on update, %v", err)
			}

			isBanInvoked := resp.Send && resp.BanInterval > 0 &&
				(!l.SuperUsers.IsSuper(resp.User.Username) || resp.ChannelID != 0) && // should not ban superusers, but ban channels
				fromChat == l.chatID // ban only in the same chat

			// some bots may request direct ban for given duration
			if isBanInvoked {
				log.Printf("[DEBUG] ban initiated for %+v", resp)
				banUserStr := getBanUsername(resp, update)

				banSuccessMessage := fmt.Sprintf("[INFO] %s banned by bot for %v", banUserStr, resp.BanInterval)
				if resp.ChannelID != 0 {
					banSuccessMessage = fmt.Sprintf("[INFO] %v channel banned by bot forever", banUserStr)
				}

				if err := l.banUserOrChannel(resp.BanInterval, fromChat, resp.User.ID, resp.ChannelID); err != nil {
					log.Printf("[ERROR] can't ban %s on bot response, %v", banUserStr, err)
				} else {
					log.Print(banSuccessMessage)
				}
			}

			// delete message if requested by bot
			if resp.DeleteReplyTo && resp.ReplyTo != 0 {
				_, err := l.TbAPI.Request(tbapi.DeleteMessageConfig{ChatID: l.chatID, MessageID: resp.ReplyTo})
				if err != nil {
					log.Printf("[WARN] failed to delete message %d, %v", resp.ReplyTo, err)
				}
			}

		case resp := <-l.msgs.ch: // publish messages from outside clients
			if err := l.sendBotResponse(resp, l.chatID); err != nil {
				log.Printf("[WARN] failed to respond on rtjc event, %v", err)
			}

		case <-time.After(l.IdleDuration): // hit bots on idle timeout
			resp := l.Bots.OnMessage(bot.Message{Text: "idle"})
			if err := l.sendBotResponse(resp, l.chatID); err != nil {
				log.Printf("[WARN] failed to respond on idle, %v", err)
			}
		}
	}
}

func getBanUsername(resp bot.Response, update tbapi.Update) string {
	if resp.ChannelID == 0 {
		return fmt.Sprintf("%v", resp.User)
	}
	botChat := bot.SenderChat{
		ID: resp.ChannelID,
	}
	if update.Message.SenderChat != nil {
		botChat.UserName = update.Message.SenderChat.UserName
	}
	// if not set, that means the ban comes from superuser and username should be taken from ReplyToMessage
	if botChat.UserName == "" && update.Message.ReplyToMessage.SenderChat != nil {
		botChat.UserName = update.Message.ReplyToMessage.SenderChat.UserName
	}
	return fmt.Sprintf("%v", botChat)
}

func (l *TelegramListener) botActivityBan(resp bot.Response, msg bot.Message, fromChat, fromID int64) bool {
	if !resp.Send {
		return false
	}

	if l.SuperUsers.IsSuper(resp.User.Username) {
		return false
	}

	// check for bot-activity ban for given users
	if b := l.BotsActivityTerm.check(msg.From, msg.SenderChat, msg.Sent, fromChat); b.active {
		if b.new {
			if err := l.applyBan(msg, l.BotsActivityTerm.BanDuration, fromChat, fromID); err != nil {
				log.Printf("[ERROR] can't ban on bot activity for given user, %v", err)
			}
		}
		return true
	}

	// check for bot-activity ban for all users
	if b := l.OverallBotActivityTerm.check(bot.User{}, bot.SenderChat{}, msg.Sent, fromChat); b.active {
		if b.new {
			if err := l.applyBan(msg, l.BotsActivityTerm.BanDuration, fromChat, fromID); err != nil {
				log.Printf("[ERROR] can't ban on bot activity for all users, %v", err)
			}
		}
		return true
	}

	return false
}

// sendBotResponse sends bot's answer to tg channel and saves it to log
func (l *TelegramListener) sendBotResponse(resp bot.Response, chatID int64) error {
	if !resp.Send {
		return nil
	}

	log.Printf("[DEBUG] bot response - %+v, pin: %t, reply-to:%d, parse-mode:%s", resp.Text, resp.Pin, resp.ReplyTo, resp.ParseMode)
	tbMsg := tbapi.NewMessage(chatID, resp.Text)
	tbMsg.ParseMode = tbapi.ModeMarkdown
	if resp.ParseMode != "" {
		tbMsg.ParseMode = resp.ParseMode
	}
	tbMsg.DisableWebPagePreview = !resp.Preview
	tbMsg.ReplyToMessageID = resp.ReplyTo
	res, err := l.TbAPI.Send(tbMsg)
	if err != nil {
		return fmt.Errorf("can't send message to telegram %q: %w", resp.Text, err)
	}

	l.saveBotMessage(&res, chatID)

	if resp.Pin {
		_, err = l.TbAPI.Request(tbapi.PinChatMessageConfig{ChatID: chatID, MessageID: res.MessageID, DisableNotification: true})
		if err != nil {
			return fmt.Errorf("can't pin message to telegram: %w", err)
		}
	}

	if resp.Unpin {
		_, err = l.TbAPI.Request(tbapi.UnpinChatMessageConfig{ChatID: chatID})
		if err != nil {
			return fmt.Errorf("can't unpin message to telegram: %w", err)
		}
	}

	return nil
}

// bans user or a channel
func (l *TelegramListener) applyBan(msg bot.Message, duration time.Duration, chatID, userID int64) error {
	mention := "@" + msg.From.Username
	if msg.From.Username == "" {
		mention = msg.From.DisplayName
	}
	m := fmt.Sprintf("%s _тебя слишком много, отдохни..._", bot.EscapeMarkDownV1Text(mention))
	banUserStr := fmt.Sprintf("%v", msg.From)
	var channelID int64
	// This userID is a bot which means that message was sent on behalf of the channel
	// https://docs.python-telegram-bot.org/en/stable/telegram.constants.html#telegram.constants.FAKE_CHANNEL_ID
	if msg.From.ID == 136817688 {
		channelID = msg.SenderChat.ID
		mention = "@" + msg.SenderChat.UserName
		banUserStr = fmt.Sprintf("%v", msg.SenderChat)
		m = fmt.Sprintf("%s _пал смертью храбрых, заблокирован навечно..._", bot.EscapeMarkDownV1Text(mention))
	}

	if err := l.sendBotResponse(bot.Response{Text: m, Send: true}, chatID); err != nil {
		return fmt.Errorf("failed to send ban message for %v: %w", msg.From, err)
	}
	err := l.banUserOrChannel(duration, chatID, userID, channelID)
	if err != nil {
		return fmt.Errorf("failed to ban user %s: %w", banUserStr, err)
	}
	return nil
}

// Submit message text to telegram's group
func (l *TelegramListener) Submit(ctx context.Context, text string, pin bool) error {
	l.msgs.once.Do(func() { l.msgs.ch = make(chan bot.Response, 100) })

	select {
	case <-ctx.Done():
		return ctx.Err()
	case l.msgs.ch <- bot.Response{Text: text, Pin: pin, Send: true, Preview: true}:
	}
	return nil
}

// SubmitHTML message to telegram's group with HTML mode
func (l *TelegramListener) SubmitHTML(ctx context.Context, text string, pin bool) error {
	// Remove unsupported HTML tags
	text = notify.TelegramSupportedHTML(text)
	l.msgs.once.Do(func() { l.msgs.ch = make(chan bot.Response, 100) })

	select {
	case <-ctx.Done():
		return ctx.Err()
	case l.msgs.ch <- bot.Response{Text: text, Pin: pin, Send: true, ParseMode: tbapi.ModeHTML, Preview: false}:
	}
	return nil
}

func (l *TelegramListener) getChatID(group string) (int64, error) {
	chatID, err := strconv.ParseInt(l.Group, 10, 64)
	if err == nil {
		return chatID, nil
	}

	chat, err := l.TbAPI.GetChat(tbapi.ChatInfoConfig{ChatConfig: tbapi.ChatConfig{SuperGroupUsername: "@" + group}})
	if err != nil {
		return 0, fmt.Errorf("can't get chat for %s: %w", group, err)
	}

	return chat.ID, nil
}

func (l *TelegramListener) saveBotMessage(msg *tbapi.Message, fromChat int64) {
	if fromChat != l.chatID {
		return
	}
	l.MsgLogger.Save(l.transform(msg))
}

// The bot must be an administrator in the supergroup for this to work
// and must have the appropriate admin rights.
// If channel is provided, it is banned instead of provided user, permanently.
func (l *TelegramListener) banUserOrChannel(duration time.Duration, chatID, userID, channelID int64) error {
	// From Telegram Bot API documentation:
	// > If user is restricted for more than 366 days or less than 30 seconds from the current time,
	// > they are considered to be restricted forever
	// Because the API query uses unix timestamp rather than "ban duration",
	// you do not want to accidentally get into this 30-second window of a lifetime ban.
	// In practice BanDuration is equal to ten minutes,
	// so this `if` statement is unlikely to be evaluated to true.
	if duration < 30*time.Second {
		duration = 1 * time.Minute
	}

	if channelID != 0 {
		resp, err := l.TbAPI.Request(tbapi.BanChatSenderChatConfig{
			ChatID:       chatID,
			SenderChatID: channelID,
			UntilDate:    int(time.Now().Add(duration).Unix()),
		})
		if err != nil {
			return err
		}
		if !resp.Ok {
			return fmt.Errorf("response is not Ok: %v", string(resp.Result))
		}
		return nil
	}

	resp, err := l.TbAPI.Request(tbapi.RestrictChatMemberConfig{
		ChatMemberConfig: tbapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: userID,
		},
		UntilDate: time.Now().Add(duration).Unix(),
		Permissions: &tbapi.ChatPermissions{
			CanSendMessages:       false,
			CanSendMediaMessages:  false,
			CanSendOtherMessages:  false,
			CanAddWebPagePreviews: false,
		},
	})
	if err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("response is not Ok: %v", string(resp.Result))
	}

	return nil
}

func (l *TelegramListener) transform(msg *tbapi.Message) *bot.Message {
	message := bot.Message{
		ID:   msg.MessageID,
		Sent: msg.Time(),
		Text: msg.Text,
	}

	if msg.Chat != nil {
		message.ChatID = msg.Chat.ID
	}

	if msg.From != nil {
		message.From = bot.User{
			ID:          msg.From.ID,
			Username:    msg.From.UserName,
			DisplayName: msg.From.FirstName + " " + msg.From.LastName,
		}
	}

	if msg.SenderChat != nil {
		message.SenderChat = bot.SenderChat{
			ID:       msg.SenderChat.ID,
			UserName: msg.SenderChat.UserName,
		}
	}

	switch {
	case msg.Entities != nil && len(msg.Entities) > 0:
		message.Entities = l.transformEntities(msg.Entities)

	case msg.Photo != nil && len(msg.Photo) > 0:
		sizes := msg.Photo
		lastSize := sizes[len(sizes)-1]
		message.Image = &bot.Image{
			FileID:   lastSize.FileID,
			Width:    lastSize.Width,
			Height:   lastSize.Height,
			Caption:  msg.Caption,
			Entities: l.transformEntities(msg.CaptionEntities),
		}
	}

	// fill in the message's reply-to message
	if msg.ReplyToMessage != nil {
		message.ReplyTo.Text = msg.ReplyToMessage.Text
		message.ReplyTo.Sent = msg.ReplyToMessage.Time()
		if msg.ReplyToMessage.From != nil {
			message.ReplyTo.From = bot.User{
				ID:          msg.ReplyToMessage.From.ID,
				Username:    msg.ReplyToMessage.From.UserName,
				DisplayName: msg.ReplyToMessage.From.FirstName + " " + msg.ReplyToMessage.From.LastName,
			}
		}
		if msg.ReplyToMessage.SenderChat != nil {
			message.ReplyTo.SenderChat = bot.SenderChat{
				ID:       msg.ReplyToMessage.SenderChat.ID,
				UserName: msg.ReplyToMessage.SenderChat.UserName,
			}
		}
	}

	return &message
}

func (l *TelegramListener) transformEntities(entities []tbapi.MessageEntity) *[]bot.Entity {
	if len(entities) == 0 {
		return nil
	}

	result := make([]bot.Entity, 0, len(entities))
	for _, entity := range entities {
		e := bot.Entity{
			Type:   entity.Type,
			Offset: entity.Offset,
			Length: entity.Length,
			URL:    entity.URL,
		}
		if entity.User != nil {
			e.User = &bot.User{
				ID:          entity.User.ID,
				Username:    entity.User.UserName,
				DisplayName: entity.User.FirstName + " " + entity.User.LastName,
			}
		}
		result = append(result, e)
	}

	return &result
}

package tgbotapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// APIResponse is a response from the Telegram API with the result
// stored raw.
type APIResponse struct {
	Ok          bool                `json:"ok"`
	Result      json.RawMessage     `json:"result,omitempty"`
	ErrorCode   int                 `json:"error_code,omitempty"`
	Description string              `json:"description,omitempty"`
	Parameters  *ResponseParameters `json:"parameters,omitempty"`
}

// Error is an error containing extra information returned by the Telegram API.
type Error struct {
	Code    int
	Message string
	ResponseParameters
}

// Error message string.
func (e Error) Error() string {
	return e.Message
}

// Update is an update response, from GetUpdates.
type Update struct {
	// UpdateID is the update's unique identifier.
	// Update identifiers start from a certain positive number and increase
	// sequentially.
	// This ID becomes especially handy if you're using Webhooks,
	// since it allows you to ignore repeated updates or to restore
	// the correct update sequence, should they get out of order.
	// If there are no new updates for at least a week, then identifier
	// of the next update will be chosen randomly instead of sequentially.
	UpdateID int `json:"update_id"`
	// Message new incoming message of any kind — text, photo, sticker, etc.
	//
	// optional
	Message *Message `json:"message,omitempty"`
	// EditedMessage new version of a message that is known to the bot and was
	// edited
	//
	// optional
	EditedMessage *Message `json:"edited_message,omitempty"`
	// ChannelPost new version of a message that is known to the bot and was
	// edited
	//
	// optional
	ChannelPost *Message `json:"channel_post,omitempty"`
	// EditedChannelPost new incoming channel post of any kind — text, photo,
	// sticker, etc.
	//
	// optional
	EditedChannelPost *Message `json:"edited_channel_post,omitempty"`
	// BusinessConnection the bot was connected to or disconnected from a
	// business account, or a user edited an existing connection with the bot
	//
	// optional
	BusinessConnection *BusinessConnection `json:"business_connection,omitempty"`
	// BusinessMessage is a new non-service message from a
	// connected business account
	//
	// optional
	BusinessMessage *Message `json:"business_message,omitempty"`
	// EditedBusinessMessage is a new version of a message from a
	// connected business account
	//
	// optional
	EditedBusinessMessage *Message `json:"edited_business_message,omitempty"`
	// DeletedBusinessMessages are the messages were deleted from a
	// connected business account
	//
	// optional
	DeletedBusinessMessages *BusinessMessagesDeleted `json:"deleted_business_messages,omitempty"`
	// MessageReaction is a reaction to a message was changed by a user.
	//
	// optional
	MessageReaction *MessageReactionUpdated `json:"message_reaction,omitempty"`
	// MessageReactionCount reactions to a message with anonymous reactions were changed.
	//
	// optional
	MessageReactionCount *MessageReactionCountUpdated `json:"message_reaction_count,omitempty"`
	// InlineQuery new incoming inline query
	//
	// optional
	InlineQuery *InlineQuery `json:"inline_query,omitempty"`
	// ChosenInlineResult is the result of an inline query
	// that was chosen by a user and sent to their chat partner.
	// Please see our documentation on the feedback collecting
	// for details on how to enable these updates for your bot.
	//
	// optional
	ChosenInlineResult *ChosenInlineResult `json:"chosen_inline_result,omitempty"`
	// CallbackQuery new incoming callback query
	//
	// optional
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
	// ShippingQuery new incoming shipping query. Only for invoices with
	// flexible price
	//
	// optional
	ShippingQuery *ShippingQuery `json:"shipping_query,omitempty"`
	// PreCheckoutQuery new incoming pre-checkout query. Contains full
	// information about checkout
	//
	// optional
	PreCheckoutQuery *PreCheckoutQuery `json:"pre_checkout_query,omitempty"`
	// PurchasedPaidMedia is user purchased paid media with a non-empty
	// payload sent by the bot in a non-channel chat
	//
	// optional
	PurchasedPaidMedia *PaidMediaPurchased `json:"purchased_paid_media,omitempty"`
	// Pool new poll state. Bots receive only updates about stopped polls and
	// polls, which are sent by the bot
	//
	// optional
	Poll *Poll `json:"poll,omitempty"`
	// PollAnswer user changed their answer in a non-anonymous poll. Bots
	// receive new votes only in polls that were sent by the bot itself.
	//
	// optional
	PollAnswer *PollAnswer `json:"poll_answer,omitempty"`
	// MyChatMember is the bot's chat member status was updated in a chat. For
	// private chats, this update is received only when the bot is blocked or
	// unblocked by the user.
	//
	// optional
	MyChatMember *ChatMemberUpdated `json:"my_chat_member,omitempty"`
	// ChatMember is a chat member's status was updated in a chat. The bot must
	// be an administrator in the chat and must explicitly specify "chat_member"
	// in the list of allowed_updates to receive these updates.
	//
	// optional
	ChatMember *ChatMemberUpdated `json:"chat_member,omitempty"`
	// ChatJoinRequest is a request to join the chat has been sent. The bot must
	// have the can_invite_users administrator right in the chat to receive
	// these updates.
	//
	// optional
	ChatJoinRequest *ChatJoinRequest `json:"chat_join_request,omitempty"`
	// ChatBoostUpdated represents a boost added to a chat or changed.
	//
	// optional
	ChatBoost *ChatBoostUpdated `json:"chat_boost,omitempty"`
	// ChatBoostRemoved represents a boost removed from a chat.
	//
	// optional
	ChatBoostRemoved *ChatBoostRemoved `json:"removed_chat_boost,omitempty"`
}

// SentFrom returns the user who sent an update. Can be nil, if Telegram did not provide information
// about the user in the update object.
func (u *Update) SentFrom() *User {
	switch {
	case u.Message != nil:
		return u.Message.From
	case u.EditedMessage != nil:
		return u.EditedMessage.From
	case u.InlineQuery != nil:
		return u.InlineQuery.From
	case u.ChosenInlineResult != nil:
		return u.ChosenInlineResult.From
	case u.CallbackQuery != nil:
		return u.CallbackQuery.From
	case u.ShippingQuery != nil:
		return u.ShippingQuery.From
	case u.PreCheckoutQuery != nil:
		return u.PreCheckoutQuery.From
	default:
		return nil
	}
}

// CallbackData returns the callback query data, if it exists.
func (u *Update) CallbackData() string {
	if u.CallbackQuery != nil {
		return u.CallbackQuery.Data
	}
	return ""
}

// FromChat returns the chat where an update occurred.
func (u *Update) FromChat() *Chat {
	switch {
	case u.Message != nil:
		return &u.Message.Chat
	case u.EditedMessage != nil:
		return &u.EditedMessage.Chat
	case u.ChannelPost != nil:
		return &u.ChannelPost.Chat
	case u.EditedChannelPost != nil:
		return &u.EditedChannelPost.Chat
	case u.CallbackQuery != nil && u.CallbackQuery.Message != nil:
		return &u.CallbackQuery.Message.Chat
	default:
		return nil
	}
}

// UpdatesChannel is the channel for getting updates.
type UpdatesChannel <-chan Update

// Clear discards all unprocessed incoming updates.
func (ch UpdatesChannel) Clear() {
	for len(ch) != 0 {
		<-ch
	}
}

// User represents a Telegram user or bot.
type User struct {
	// ID is a unique identifier for this user or bot
	ID int64 `json:"id"`
	// IsBot true, if this user is a bot
	//
	// optional
	IsBot bool `json:"is_bot,omitempty"`
	// IsPremium true, if user has Telegram Premium
	//
	// optional
	IsPremium bool `json:"is_premium,omitempty"`
	// AddedToAttachmentMenu true, if this user added the bot to the attachment menu
	//
	// optional
	AddedToAttachmentMenu bool `json:"added_to_attachment_menu,omitempty"`
	// FirstName user's or bot's first name
	FirstName string `json:"first_name"`
	// LastName user's or bot's last name
	//
	// optional
	LastName string `json:"last_name,omitempty"`
	// UserName user's or bot's username
	//
	// optional
	UserName string `json:"username,omitempty"`
	// LanguageCode IETF language tag of the user's language
	// more info: https://en.wikipedia.org/wiki/IETF_language_tag
	//
	// optional
	LanguageCode string `json:"language_code,omitempty"`
	// CanJoinGroups is true, if the bot can be invited to groups.
	// Returned only in getMe.
	//
	// optional
	CanJoinGroups bool `json:"can_join_groups,omitempty"`
	// CanReadAllGroupMessages is true, if privacy mode is disabled for the bot.
	// Returned only in getMe.
	//
	// optional
	CanReadAllGroupMessages bool `json:"can_read_all_group_messages,omitempty"`
	// SupportsInlineQueries is true, if the bot supports inline queries.
	// Returned only in getMe.
	//
	// optional
	SupportsInlineQueries bool `json:"supports_inline_queries,omitempty"`
	// CanConnectToBusiness is true, if the bot can be connected to a
	// Telegram Business account to receive its messages.
	// Returned only in getMe.
	//
	// optional
	CanConnectToBusiness bool `json:"can_connect_to_business,omitempty"`
	// True, if the bot has a main Web App. Returned only in getMe.
	//
	// optional
	HasMainWebApp bool `json:"has_main_web_app,omitempty"`
}

// String displays a simple text version of a user.
//
// It is normally a user's username, but falls back to a first/last
// name as available.
func (u *User) String() string {
	if u == nil {
		return ""
	}
	if u.UserName != "" {
		return u.UserName
	}

	name := u.FirstName
	if u.LastName != "" {
		name += " " + u.LastName
	}

	return name
}

// Chat represents a chat.
type Chat struct {
	// ID is a unique identifier for this chat
	ID int64 `json:"id"`
	// Type of chat, can be either “private”, “group”, “supergroup” or “channel”
	Type string `json:"type"`
	// Title for supergroups, channels and group chats
	//
	// optional
	Title string `json:"title,omitempty"`
	// UserName for private chats, supergroups and channels if available
	//
	// optional
	UserName string `json:"username,omitempty"`
	// FirstName of the other party in a private chat
	//
	// optional
	FirstName string `json:"first_name,omitempty"`
	// LastName of the other party in a private chat
	//
	// optional
	LastName string `json:"last_name,omitempty"`
	// IsForum is true if the supergroup chat is a forum (has topics enabled)
	//
	// optional
	IsForum bool `json:"is_forum,omitempty"`
}

// ChatFullInfo contains full information about a chat.
type ChatFullInfo struct {
	Chat
	// Photo is a chat photo
	Photo *ChatPhoto `json:"photo"`
	// If non-empty, the list of all active chat usernames;
	// for private chats, supergroups and channels. Returned only in getChat.
	//
	// optional
	ActiveUsernames []string `json:"active_usernames,omitempty"`
	// Birthdate for private chats, the date of birth of the user.
	// Returned only in getChat.
	//
	// optional
	Birthdate *Birthdate `json:"birthdate,omitempty"`
	// BusinessIntro is for private chats with business accounts, the intro of the business.
	// Returned only in getChat.
	//
	// optional
	BusinessIntro *BusinessIntro `json:"business_intro,omitempty"`
	// BusinessLocation is for private chats with business accounts, the location
	// of the business. Returned only in getChat.
	//
	// optional
	BusinessLocation *BusinessLocation `json:"business_location,omitempty"`
	// BusinessOpeningHours is for private chats with business accounts,
	// the opening hours of the business. Returned only in getChat.
	//
	// optional
	BusinessOpeningHours *BusinessOpeningHours `json:"business_opening_hours,omitempty"`
	// PersonalChat is for private chats, the personal channel of the user.
	// Returned only in getChat.
	//
	// optional
	PersonalChat *Chat `json:"personal_chat,omitempty"`
	// AvailableReactions is a list of available reactions allowed in the chat.
	// If omitted, then all emoji reactions are allowed. Returned only in getChat.
	//
	// optional
	AvailableReactions []ReactionType `json:"available_reactions,omitempty"`
	// AccentColorID is an identifier of the accent color for the chat name and backgrounds of
	// the chat photo, reply header, and link preview.
	// See accent colors for more details. Returned only in getChat.
	// Always returned in getChat.
	//
	// optional
	AccentColorID int `json:"accent_color_id,omitempty"`
	// The maximum number of reactions that can be set on a message in the chat
	MaxReactionCount int `json:"max_reaction_count"`
	// BackgroundCustomEmojiID is a custom emoji identifier of emoji chosen by
	// the chat for the reply header and link preview background.
	// Returned only in getChat.
	//
	// optional
	BackgroundCustomEmojiID string `json:"background_custom_emoji_id,omitempty"`
	// ProfileAccentColorID is ani dentifier of the accent color for the chat's profile background.
	// See profile accent colors for more details. Returned only in getChat.
	//
	// optional
	ProfileAccentColorID int `json:"profile_accent_color_id,omitempty"`
	// ProfileBackgroundCustomEmojiID is a custom emoji identifier of the emoji chosen by
	// the chat for its profile background. Returned only in getChat.
	//
	// optional
	ProfileBackgroundCustomEmojiID string `json:"profile_background_custom_emoji_id,omitempty"`
	// EmojiStatusCustomEmojiID is a custom emoji identifier of emoji status of the other party
	// in a private chat. Returned only in getChat.
	//
	// optional
	EmojiStatusCustomEmojiID string `json:"emoji_status_custom_emoji_id,omitempty"`
	// EmojiStatusExpirationDate is a date of the emoji status of the chat or the other party
	// in a private chat, in Unix time, if any. Returned only in getChat.
	//
	// optional
	EmojiStatusExpirationDate int64 `json:"emoji_status_expiration_date,omitempty"`
	// Bio is the bio of the other party in a private chat. Returned only in
	// getChat
	//
	// optional
	Bio string `json:"bio,omitempty"`
	// HasPrivateForwards is true if privacy settings of the other party in the
	// private chat allows to use tg://user?id=<user_id> links only in chats
	// with the user. Returned only in getChat.
	//
	// optional
	HasPrivateForwards bool `json:"has_private_forwards,omitempty"`
	// HasRestrictedVoiceAndVideoMessages if the privacy settings of the other party
	// restrict sending voice and video note messages
	// in the private chat. Returned only in getChat.
	//
	// optional
	HasRestrictedVoiceAndVideoMessages bool `json:"has_restricted_voice_and_video_messages,omitempty"`
	// JoinToSendMessages is true, if users need to join the supergroup
	// before they can send messages.
	// Returned only in getChat
	//
	// optional
	JoinToSendMessages bool `json:"join_to_send_messages,omitempty"`
	// JoinByRequest is true, if all users directly joining the supergroup
	// need to be approved by supergroup administrators.
	// Returned only in getChat.
	//
	// optional
	JoinByRequest bool `json:"join_by_request,omitempty"`
	// Description for groups, supergroups and channel chats
	//
	// optional
	Description string `json:"description,omitempty"`
	// InviteLink is a chat invite link, for groups, supergroups and channel chats.
	// Each administrator in a chat generates their own invite links,
	// so the bot must first generate the link using exportChatInviteLink
	//
	// optional
	InviteLink string `json:"invite_link,omitempty"`
	// PinnedMessage is the pinned message, for groups, supergroups and channels
	//
	// optional
	PinnedMessage *Message `json:"pinned_message,omitempty"`
	// Permissions are default chat member permissions, for groups and
	// supergroups. Returned only in getChat.
	//
	// optional
	Permissions *ChatPermissions `json:"permissions,omitempty"`
	// True, if paid media messages can be sent or forwarded to the channel chat.
	// The field is available only for channel chats.
	//
	// optional
	CanSendPaidMedia bool `json:"can_send_paid_media,omitempty"`
	// SlowModeDelay is for supergroups, the minimum allowed delay between
	// consecutive messages sent by each unprivileged user. Returned only in
	// getChat.
	//
	// optional
	SlowModeDelay int `json:"slow_mode_delay,omitempty"`
	// UnrestrictBoostCount  is for supergroups, the minimum number of boosts that
	// a non-administrator user needs to add in order to
	// ignore slow mode and chat permissions. Returned only in getChat.
	//
	// optional
	UnrestrictBoostCount int `json:"unrestrict_boost_count,omitempty"`
	// MessageAutoDeleteTime is the time after which all messages sent to the
	// chat will be automatically deleted; in seconds. Returned only in getChat.
	//
	// optional
	MessageAutoDeleteTime int `json:"message_auto_delete_time,omitempty"`
	// HasAggressiveAntiSpamEnabled is true if aggressive anti-spam checks are enabled
	// in the supergroup. The field is only available to chat administrators.
	// Returned only in getChat.
	//
	// optional
	HasAggressiveAntiSpamEnabled bool `json:"has_aggressive_anti_spam_enabled,omitempty"`
	// HasHiddenMembers is true if non-administrators can only get
	// the list of bots and administrators in the chat.
	//
	// optional
	HasHiddenMembers bool `json:"has_hidden_members,omitempty"`
	// HasProtectedContent is true if messages from the chat can't be forwarded
	// to other chats. Returned only in getChat.
	//
	// optional
	HasProtectedContent bool `json:"has_protected_content,omitempty"`
	// HasVisibleHistory is True, if new chat members will have access to old messages;
	// available only to chat administrators. Returned only in getChat.
	//
	// optional
	HasVisibleHistory bool `json:"has_visible_history,omitempty"`
	// StickerSetName is for supergroups, name of group sticker set.Returned
	// only in getChat.
	//
	// optional
	StickerSetName string `json:"sticker_set_name,omitempty"`
	// CanSetStickerSet is true, if the bot can change the group sticker set.
	// Returned only in getChat.
	//
	// optional
	CanSetStickerSet bool `json:"can_set_sticker_set,omitempty"`
	// CustomEmojiStickerSetName is for supergroups, the name of the group's
	// custom emoji sticker set. Custom emoji from this set can be used by all
	// users and bots in the group. Returned only in getChat.
	//
	// optional
	CustomEmojiStickerSetName string `json:"custom_emoji_sticker_set_name,omitempty"`
	// LinkedChatID is a unique identifier for the linked chat, i.e. the
	// discussion group identifier for a channel and vice versa; for supergroups
	// and channel chats.
	//
	// optional
	LinkedChatID int64 `json:"linked_chat_id,omitempty"`
	// Location is for supergroups, the location to which the supergroup is
	// connected. Returned only in getChat.
	//
	// optional
	Location *ChatLocation `json:"location,omitempty"`
}

// IsPrivate returns if the Chat is a private conversation.
func (c Chat) IsPrivate() bool {
	return c.Type == "private"
}

// IsGroup returns if the Chat is a group.
func (c Chat) IsGroup() bool {
	return c.Type == "group"
}

// IsSuperGroup returns if the Chat is a supergroup.
func (c Chat) IsSuperGroup() bool {
	return c.Type == "supergroup"
}

// IsChannel returns if the Chat is a channel.
func (c Chat) IsChannel() bool {
	return c.Type == "channel"
}

// ChatConfig returns a ChatConfig struct for chat related methods.
func (c Chat) ChatConfig() ChatConfig {
	return ChatConfig{ChatID: c.ID}
}

// InaccessibleMessage describes a message that was deleted or is otherwise inaccessible to the bot.
type InaccessibleMessage struct {
	// Chat the message belonged to
	Chat Chat `json:"chat"`
	// MessageID is unique message identifier inside the chat
	MessageID int `json:"message_id"`
	// Date is always 0. The field can be used to differentiate regular and inaccessible messages.
	Date int `json:"date"`
}

// Message represents a message.
type Message struct {
	// MessageID is a unique message identifier inside this chat
	MessageID int `json:"message_id"`
	// Unique identifier of a message thread to which the message belongs;
	// for supergroups only
	//
	// optional
	MessageThreadID int `json:"message_thread_id,omitempty"`
	// From is a sender, empty for messages sent to channels;
	//
	// optional
	From *User `json:"from,omitempty"`
	// SenderChat is the sender of the message, sent on behalf of a chat. The
	// channel itself for channel messages. The supergroup itself for messages
	// from anonymous group administrators. The linked channel for messages
	// automatically forwarded to the discussion group
	//
	// optional
	SenderChat *Chat `json:"sender_chat,omitempty"`
	// SenderBoostCount is the number of boosts added by the user,
	// if the sender of the message boosted the chat
	//
	// optional
	SenderBoostCount int `json:"sender_boost_count,omitempty"`
	// SenderBusinessBot is the bot that actually sent the message on behalf of
	// the business account. Available only for outgoing messages sent on
	// behalf of the connected business account.
	//
	// optional
	SenderBusinessBot *User `json:"sender_business_bot,omitempty"`
	// Date of the message was sent in Unix time
	Date int `json:"date"`
	// BusinessConnectionID is an unique identifier of the business connection
	// from which the message was received. If non-empty, the message belongs to
	// a chat of the corresponding business account that is independent from
	// any potential bot chat which might share the same identifier.
	//
	// optional
	BusinessConnectionID string `json:"business_connection_id,omitempty"`
	// Chat is the conversation the message belongs to
	Chat Chat `json:"chat"`
	// ForwardOrigin is information about the original message for forwarded messages
	//
	// optional
	ForwardOrigin *MessageOrigin `json:"forward_origin,omitempty"`
	// IsTopicMessage true if the message is sent to a forum topic
	//
	// optional
	IsTopicMessage bool `json:"is_topic_message,omitempty"`
	// IsAutomaticForward is true if the message is a channel post that was
	// automatically forwarded to the connected discussion group.
	//
	// optional
	IsAutomaticForward bool `json:"is_automatic_forward,omitempty"`
	// ReplyToMessage for replies, the original message.
	// Note that the Message object in this field will not contain further ReplyToMessage fields
	// even if it itself is a reply;
	//
	// optional
	ReplyToMessage *Message `json:"reply_to_message,omitempty"`
	// ExternalReply is an information about the message that is being replied to,
	// which may come from another chat or forum topic.
	//
	// optional
	ExternalReply *ExternalReplyInfo `json:"external_reply,omitempty"`
	// Quote for replies that quote part of the original message, the quoted part of the message
	//
	// optional
	Quote *TextQuote `json:"text_quote,omitempty"`
	// ReplyToStory for replies to a story, the original story
	//
	// ReplyToStory
	ReplyToStory *Story `json:"reply_to_story"`
	// ViaBot through which the message was sent;
	//
	// optional
	ViaBot *User `json:"via_bot,omitempty"`
	// EditDate of the message was last edited in Unix time;
	//
	// optional
	EditDate int `json:"edit_date,omitempty"`
	// HasProtectedContent is true if the message can't be forwarded.
	//
	// optional
	HasProtectedContent bool `json:"has_protected_content,omitempty"`
	// IsFromOffline is True, if the message was sent by an implicit action,
	// for example, as an away or a greeting business message, or as a scheduled message
	//
	// optional
	IsFromOffline bool `json:"is_from_offline,omitempty"`
	// MediaGroupID is the unique identifier of a media message group this message belongs to;
	//
	// optional
	MediaGroupID string `json:"media_group_id,omitempty"`
	// AuthorSignature is the signature of the post author for messages in channels;
	//
	// optional
	AuthorSignature string `json:"author_signature,omitempty"`
	// Text is for text messages, the actual UTF-8 text of the message, 0-4096 characters;
	//
	// optional
	Text string `json:"text,omitempty"`
	// Entities are for text messages, special entities like usernames,
	// URLs, bot commands, etc. that appear in the text;
	//
	// optional
	Entities []MessageEntity `json:"entities,omitempty"`
	// LinkPreviewOptions are options used for link preview generation for the message,
	// if it is a text message and link preview options were changed
	//
	// Optional
	LinkPreviewOptions *LinkPreviewOptions `json:"link_preview_options,omitempty"`
	// EffectID is the unique identifier of the message effect added to the message
	//
	// optional
	EffectID string `json:"effect_id,omitempty"`
	// Animation message is an animation, information about the animation.
	// For backward compatibility, when this field is set, the document field will also be set;
	//
	// optional
	Animation *Animation `json:"animation,omitempty"`
	// PremiumAnimation message is an animation, information about the animation.
	// For backward compatibility, when this field is set, the document field will also be set;
	//
	// optional
	PremiumAnimation *Animation `json:"premium_animation,omitempty"`
	// Audio message is an audio file, information about the file;
	//
	// optional
	Audio *Audio `json:"audio,omitempty"`
	// Document message is a general file, information about the file;
	//
	// optional
	Document *Document `json:"document,omitempty"`
	// Message contains paid media; information about the paid media
	//
	// optional
	PaidMedia *PaidMediaInfo `json:"paid_media,omitempty"`
	// Photo message is a photo, available sizes of the photo;
	//
	// optional
	Photo []PhotoSize `json:"photo,omitempty"`
	// Sticker message is a sticker, information about the sticker;
	//
	// optional
	Sticker *Sticker `json:"sticker,omitempty"`
	// Story message is a forwarded story;
	//
	// optional
	Story *Story `json:"story,omitempty"`
	// Video message is a video, information about the video;
	//
	// optional
	Video *Video `json:"video,omitempty"`
	// VideoNote message is a video note, information about the video message;
	//
	// optional
	VideoNote *VideoNote `json:"video_note,omitempty"`
	// Voice message is a voice message, information about the file;
	//
	// optional
	Voice *Voice `json:"voice,omitempty"`
	// Caption for the animation, audio, document, photo, video or voice, 0-1024 characters;
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// CaptionEntities;
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// ShowCaptionAboveMedia is True, if the caption must be shown above the message media
	//
	// optional
	ShowCaptionAboveMedia bool `json:"show_caption_above_media,omitempty"`
	// HasSpoiler True, if the message media is covered by a spoiler animation
	//
	// optional
	HasMediaSpoiler bool `json:"has_media_spoiler,omitempty"`
	// Contact message is a shared contact, information about the contact;
	//
	// optional
	Contact *Contact `json:"contact,omitempty"`
	// Dice is a dice with random value;
	//
	// optional
	Dice *Dice `json:"dice,omitempty"`
	// Game message is a game, information about the game;
	//
	// optional
	Game *Game `json:"game,omitempty"`
	// Poll is a native poll, information about the poll;
	//
	// optional
	Poll *Poll `json:"poll,omitempty"`
	// Venue message is a venue, information about the venue.
	// For backward compatibility, when this field is set, the location field
	// will also be set;
	//
	// optional
	Venue *Venue `json:"venue,omitempty"`
	// Location message is a shared location, information about the location;
	//
	// optional
	Location *Location `json:"location,omitempty"`
	// NewChatMembers that were added to the group or supergroup
	// and information about them (the bot itself may be one of these members);
	//
	// optional
	NewChatMembers []User `json:"new_chat_members,omitempty"`
	// LeftChatMember is a member was removed from the group,
	// information about them (this member may be the bot itself);
	//
	// optional
	LeftChatMember *User `json:"left_chat_member,omitempty"`
	// NewChatTitle is a chat title was changed to this value;
	//
	// optional
	NewChatTitle string `json:"new_chat_title,omitempty"`
	// NewChatPhoto is a chat photo was change to this value;
	//
	// optional
	NewChatPhoto []PhotoSize `json:"new_chat_photo,omitempty"`
	// DeleteChatPhoto is a service message: the chat photo was deleted;
	//
	// optional
	DeleteChatPhoto bool `json:"delete_chat_photo,omitempty"`
	// GroupChatCreated is a service message: the group has been created;
	//
	// optional
	GroupChatCreated bool `json:"group_chat_created,omitempty"`
	// SuperGroupChatCreated is a service message: the supergroup has been created.
	// This field can't be received in a message coming through updates,
	// because bot can't be a member of a supergroup when it is created.
	// It can only be found in ReplyToMessage if someone replies to a very first message
	// in a directly created supergroup;
	//
	// optional
	SuperGroupChatCreated bool `json:"supergroup_chat_created,omitempty"`
	// ChannelChatCreated is a service message: the channel has been created.
	// This field can't be received in a message coming through updates,
	// because bot can't be a member of a channel when it is created.
	// It can only be found in ReplyToMessage
	// if someone replies to a very first message in a channel;
	//
	// optional
	ChannelChatCreated bool `json:"channel_chat_created,omitempty"`
	// MessageAutoDeleteTimerChanged is a service message: auto-delete timer
	// settings changed in the chat.
	//
	// optional
	MessageAutoDeleteTimerChanged *MessageAutoDeleteTimerChanged `json:"message_auto_delete_timer_changed,omitempty"`
	// MigrateToChatID is the group has been migrated to a supergroup with the specified identifier.
	// This number may be greater than 32 bits and some programming languages
	// may have difficulty/silent defects in interpreting it.
	// But it is smaller than 52 bits, so a signed 64-bit integer
	// or double-precision float type are safe for storing this identifier;
	//
	// optional
	MigrateToChatID int64 `json:"migrate_to_chat_id,omitempty"`
	// MigrateFromChatID is the supergroup has been migrated from a group with the specified identifier.
	// This number may be greater than 32 bits and some programming languages
	// may have difficulty/silent defects in interpreting it.
	// But it is smaller than 52 bits, so a signed 64-bit integer
	// or double-precision float type are safe for storing this identifier;
	//
	// optional
	MigrateFromChatID int64 `json:"migrate_from_chat_id,omitempty"`
	// Specified message was pinned.
	// Note that the Message object in this field will not contain
	// further reply_to_message fields even if it itself is a reply.
	//
	// optional
	PinnedMessage *Message `json:"pinned_message,omitempty"`
	// Invoice message is an invoice for a payment;
	//
	// optional
	Invoice *Invoice `json:"invoice,omitempty"`
	// SuccessfulPayment message is a service message about a successful payment,
	// information about the payment;
	//
	// optional
	SuccessfulPayment *SuccessfulPayment `json:"successful_payment,omitempty"`
	// Message is a service message about a refunded payment, information about the payment
	//
	// optional
	RefundedPayment *RefundedPayment `json:"refunded_payment,omitempty"`
	// UsersShared is a service message: the users were shared with the bot
	//
	// optional
	UsersShared *UsersShared `json:"users_shared,omitempty"`
	// ChatShared is a service message: a chat was shared with the bot
	//
	// optional
	ChatShared *ChatShared `json:"chat_shared,omitempty"`
	// ConnectedWebsite is the domain name of the website on which the user has
	// logged in;
	//
	// optional
	ConnectedWebsite string `json:"connected_website,omitempty"`
	// WriteAccessAllowed is a service message: the user allowed the bot
	// added to the attachment menu to write messages
	//
	// optional
	WriteAccessAllowed *WriteAccessAllowed `json:"write_access_allowed,omitempty"`
	// PassportData is a Telegram Passport data;
	//
	// optional
	PassportData *PassportData `json:"passport_data,omitempty"`
	// ProximityAlertTriggered is a service message. A user in the chat
	// triggered another user's proximity alert while sharing Live Location
	//
	// optional
	ProximityAlertTriggered *ProximityAlertTriggered `json:"proximity_alert_triggered,omitempty"`
	// BoostAdded is a service message: user boosted the chat
	//
	// optional
	BoostAdded *ChatBoostAdded `json:"boost_added,omitempty"`
	// Service message: chat background set
	//
	// optional
	ChatBackgroundSet *ChatBackground `json:"chat_background_set,omitempty"`
	// ForumTopicCreated is a service message: forum topic created
	//
	// optional
	ForumTopicCreated *ForumTopicCreated `json:"forum_topic_created,omitempty"`
	// ForumTopicClosed is a service message: forum topic edited
	//
	// optional
	ForumTopicEdited *ForumTopicEdited `json:"forum_topic_edited,omitempty"`
	// ForumTopicClosed is a service message: forum topic closed
	//
	// optional
	ForumTopicClosed *ForumTopicClosed `json:"forum_topic_closed,omitempty"`
	// ForumTopicReopened is a service message: forum topic reopened
	//
	// optional
	ForumTopicReopened *ForumTopicReopened `json:"forum_topic_reopened,omitempty"`
	// GeneralForumTopicHidden is a service message: the 'General' forum topic hidden
	//
	// optional
	GeneralForumTopicHidden *GeneralForumTopicHidden `json:"general_forum_topic_hidden,omitempty"`
	// GeneralForumTopicUnhidden is a service message: the 'General' forum topic unhidden
	//
	// optional
	GeneralForumTopicUnhidden *GeneralForumTopicUnhidden `json:"general_forum_topic_unhidden,omitempty"`
	// GiveawayCreated is as service message: a scheduled giveaway was created
	//
	// optional
	GiveawayCreated *GiveawayCreated `json:"giveaway_created,omitempty"`
	// Giveaway is a scheduled giveaway message
	//
	// optional
	Giveaway *Giveaway `json:"giveaway,omitempty"`
	// GiveawayWinners is a giveaway with public winners was completed
	//
	// optional
	GiveawayWinners *GiveawayWinners `json:"giveaway_winners,omitempty"`
	// GiveawayCompleted is a service message: a giveaway without public winners was completed
	//
	// optional
	GiveawayCompleted *GiveawayCompleted `json:"giveaway_completed,omitempty"`
	// VideoChatScheduled is a service message: video chat scheduled.
	//
	// optional
	VideoChatScheduled *VideoChatScheduled `json:"video_chat_scheduled,omitempty"`
	// VideoChatStarted is a service message: video chat started.
	//
	// optional
	VideoChatStarted *VideoChatStarted `json:"video_chat_started,omitempty"`
	// VideoChatEnded is a service message: video chat ended.
	//
	// optional
	VideoChatEnded *VideoChatEnded `json:"video_chat_ended,omitempty"`
	// VideoChatParticipantsInvited is a service message: new participants
	// invited to a video chat.
	//
	// optional
	VideoChatParticipantsInvited *VideoChatParticipantsInvited `json:"video_chat_participants_invited,omitempty"`
	// WebAppData is a service message: data sent by a Web App.
	//
	// optional
	WebAppData *WebAppData `json:"web_app_data,omitempty"`
	// ReplyMarkup is the Inline keyboard attached to the message.
	// login_url buttons are represented as ordinary url buttons.
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

// Time converts the message timestamp into a Time.
func (m *Message) Time() time.Time {
	return time.Unix(int64(m.Date), 0)
}

// IsCommand returns true if message starts with a "bot_command" entity.
func (m *Message) IsCommand() bool {
	if m.Entities == nil || len(m.Entities) == 0 {
		return false
	}

	entity := m.Entities[0]
	return entity.Offset == 0 && entity.IsCommand()
}

// Command checks if the message was a command and if it was, returns the
// command. If the Message was not a command, it returns an empty string.
//
// If the command contains the at name syntax, it is removed. Use
// CommandWithAt() if you do not want that.
func (m *Message) Command() string {
	command := m.CommandWithAt()

	if i := strings.Index(command, "@"); i != -1 {
		command = command[:i]
	}

	return command
}

// CommandWithAt checks if the message was a command and if it was, returns the
// command. If the Message was not a command, it returns an empty string.
//
// If the command contains the at name syntax, it is not removed. Use Command()
// if you want that.
func (m *Message) CommandWithAt() string {
	if !m.IsCommand() {
		return ""
	}

	// IsCommand() checks that the message begins with a bot_command entity
	entity := m.Entities[0]
	return m.Text[1:entity.Length]
}

// CommandArguments checks if the message was a command and if it was,
// returns all text after the command name. If the Message was not a
// command, it returns an empty string.
//
// Note: The first character after the command name is omitted:
// - "/foo bar baz" yields "bar baz", not " bar baz"
// - "/foo-bar baz" yields "bar baz", too
// Even though the latter is not a command conforming to the spec, the API
// marks "/foo" as command entity.
func (m *Message) CommandArguments() string {
	if !m.IsCommand() {
		return ""
	}

	// IsCommand() checks that the message begins with a bot_command entity
	entity := m.Entities[0]

	if len(m.Text) == entity.Length {
		return "" // The command makes up the whole message
	}

	return m.Text[entity.Length+1:]
}

// MessageID represents a unique message identifier.
type MessageID struct {
	MessageID int `json:"message_id"`
}

// MessageEntity represents one special entity in a text message.
type MessageEntity struct {
	// Type of the entity.
	// Can be:
	//  “mention” (@username),
	//  “hashtag” (#hashtag),
	//  “cashtag” ($USD),
	//  “bot_command” (/start@jobs_bot),
	//  “url” (https://telegram.org),
	//  “email” (do-not-reply@telegram.org),
	//  “phone_number” (+1-212-555-0123),
	//  “bold” (bold text),
	//  “italic” (italic text),
	//  “underline” (underlined text),
	//  “strikethrough” (strikethrough text),
	//  "spoiler" (spoiler message),
	//  “blockquote” (block quotation),
	//  “expandable_blockquote” (collapsed-by-default block quotation),
	//  “code” (monowidth string),
	//  “pre” (monowidth block),
	//  “text_link” (for clickable text URLs),
	//  “text_mention” (for users without usernames)
	//  “text_mention” (for inline custom emoji stickers)
	Type string `json:"type"`
	// Offset in UTF-16 code units to the start of the entity
	Offset int `json:"offset"`
	// Length
	Length int `json:"length"`
	// URL for “text_link” only, url that will be opened after user taps on the text
	//
	// optional
	URL string `json:"url,omitempty"`
	// User for “text_mention” only, the mentioned user
	//
	// optional
	User *User `json:"user,omitempty"`
	// Language for “pre” only, the programming language of the entity text
	//
	// optional
	Language string `json:"language,omitempty"`
	// CustomEmojiID for “custom_emoji” only, unique identifier of the custom emoji
	//
	// optional
	CustomEmojiID string `json:"custom_emoji_id"`
}

// ParseURL attempts to parse a URL contained within a MessageEntity.
func (e MessageEntity) ParseURL() (*url.URL, error) {
	if e.URL == "" {
		return nil, errors.New(ErrBadURL)
	}

	return url.Parse(e.URL)
}

// IsMention returns true if the type of the message entity is "mention" (@username).
func (e MessageEntity) IsMention() bool {
	return e.Type == "mention"
}

// IsTextMention returns true if the type of the message entity is "text_mention"
// (At this time, the user field exists, and occurs when tagging a member without a username)
func (e MessageEntity) IsTextMention() bool {
	return e.Type == "text_mention"
}

// IsHashtag returns true if the type of the message entity is "hashtag".
func (e MessageEntity) IsHashtag() bool {
	return e.Type == "hashtag"
}

// IsCommand returns true if the type of the message entity is "bot_command".
func (e MessageEntity) IsCommand() bool {
	return e.Type == "bot_command"
}

// IsURL returns true if the type of the message entity is "url".
func (e MessageEntity) IsURL() bool {
	return e.Type == "url"
}

// IsEmail returns true if the type of the message entity is "email".
func (e MessageEntity) IsEmail() bool {
	return e.Type == "email"
}

// IsBold returns true if the type of the message entity is "bold" (bold text).
func (e MessageEntity) IsBold() bool {
	return e.Type == "bold"
}

// IsItalic returns true if the type of the message entity is "italic" (italic text).
func (e MessageEntity) IsItalic() bool {
	return e.Type == "italic"
}

// IsCode returns true if the type of the message entity is "code" (monowidth string).
func (e MessageEntity) IsCode() bool {
	return e.Type == "code"
}

// IsPre returns true if the type of the message entity is "pre" (monowidth block).
func (e MessageEntity) IsPre() bool {
	return e.Type == "pre"
}

// IsTextLink returns true if the type of the message entity is "text_link" (clickable text URL).
func (e MessageEntity) IsTextLink() bool {
	return e.Type == "text_link"
}

// TextQuote contains information about the quoted part of a message
// that is replied to by the given message
type TextQuote struct {
	// Text of the quoted part of a message that is replied to by the given message
	Text string `json:"text"`
	// Entities special entities that appear in the quote.
	// Currently, only bold, italic, underline, strikethrough, spoiler,
	// and custom_emoji entities are kept in quotes.
	//
	// optional
	Entities []MessageEntity `json:"entities,omitempty"`
	// Position is approximate quote position in the original message
	// in UTF-16 code units as specified by the sender
	Position int `json:"position"`
	// IsManual True, if the quote was chosen manually by the message sender.
	// Otherwise, the quote was added automatically by the server.
	//
	// optional
	IsManual bool `json:"is_manual,omitempty"`
}

// ExternalReplyInfo contains information about a message that is being replied to,
// which may come from another chat or forum topic.
type ExternalReplyInfo struct {
	// Origin of the message replied to by the given message
	Origin MessageOrigin `json:"origin"`
	// Chat is the conversation the message belongs to
	Chat *Chat `json:"chat"`
	// MessageID is a unique message identifier inside this chat
	MessageID int `json:"message_id"`
	// LinkPreviewOptions used for link preview generation for the original message,
	// if it is a text message
	//
	// Optional
	LinkPreviewOptions *LinkPreviewOptions `json:"link_preview_options,omitempty"`
	// Animation message is an animation, information about the animation.
	// For backward compatibility, when this field is set, the document field will also be set;
	//
	// optional
	Animation *Animation `json:"animation,omitempty"`
	// Audio message is an audio file, information about the file;
	//
	// optional
	Audio *Audio `json:"audio,omitempty"`
	// Document message is a general file, information about the file;
	//
	// optional
	Document *Document `json:"document,omitempty"`
	// Message contains paid media; information about the paid media
	//
	// optional
	PaidMedia *PaidMediaInfo `json:"paid_media,omitempty"`
	// Photo message is a photo, available sizes of the photo;
	//
	// optional
	Photo []PhotoSize `json:"photo,omitempty"`
	// Sticker message is a sticker, information about the sticker;
	//
	// optional
	Sticker *Sticker `json:"sticker,omitempty"`
	// Story message is a forwarded story;
	//
	// optional
	Story *Story `json:"story,omitempty"`
	// Video message is a video, information about the video;
	//
	// optional
	Video *Video `json:"video,omitempty"`
	// VideoNote message is a video note, information about the video message;
	//
	// optional
	VideoNote *VideoNote `json:"video_note,omitempty"`
	// Voice message is a voice message, information about the file;
	//
	// optional
	Voice *Voice `json:"voice,omitempty"`
	// HasMediaSpoiler True, if the message media is covered by a spoiler animation
	//
	// optional
	HasMediaSpoiler bool `json:"has_media_spoiler,omitempty"`
	// Contact message is a shared contact, information about the contact;
	//
	// optional
	Contact *Contact `json:"contact,omitempty"`
	// Dice is a dice with random value;
	//
	// optional
	Dice *Dice `json:"dice,omitempty"`
	// Game message is a game, information about the game;
	//
	// optional
	Game *Game `json:"game,omitempty"`
	// Giveaway is information about the giveaway
	//
	// optional
	Giveaway *Giveaway `json:"giveaway,omitempty"`
	// GiveawayWinners a giveaway with public winners was completed
	//
	// optional
	GiveawayWinners *GiveawayWinners `json:"giveaway_winners,omitempty"`
	// Invoice message is an invoice for a payment;
	//
	// optional
	Invoice *Invoice `json:"invoice,omitempty"`
	// Location message is a shared location, information about the location;
	//
	// optional
	Location *Location `json:"location,omitempty"`
	// Poll is a native poll, information about the poll;
	//
	// optional
	Poll *Poll `json:"poll,omitempty"`
	// Venue message is a venue, information about the venue.
	// For backward compatibility, when this field is set, the location field
	// will also be set;
	//
	// optional
	Venue *Venue `json:"venue,omitempty"`
}

// ReplyParameters describes reply parameters for the message that is being sent.
type ReplyParameters struct {
	// MessageID identifier of the message that will be replied to in
	// the current chat, or in the chat chat_id if it is specified
	MessageID int `json:"message_id"`
	// ChatID if the message to be replied to is from a different chat,
	// unique identifier for the chat or username of the channel (in the format @channelusername)
	//
	// optional
	ChatID interface{} `json:"chat_id,omitempty"`
	// AllowSendingWithoutReply true if the message should be sent even
	// if the specified message to be replied to is not found;
	// can be used only for replies in the same chat and forum topic.
	//
	// optional
	AllowSendingWithoutReply bool `json:"allow_sending_without_reply,omitempty"`
	// Quote is a quoted part of the message to be replied to;
	// 0-1024 characters after entities parsing. The quote must be
	// an exact substring of the message to be replied to,
	// including bold, italic, underline, strikethrough, spoiler, and custom_emoji entities.
	// The message will fail to send if the quote isn't found in the original message.
	//
	// optional
	Quote string `json:"quote,omitempty"`
	// QuoteParseMode mode for parsing entities in the quote.
	//
	// optional
	QuoteParseMode string `json:"quote_parse_mode,omitempty"`
	// QuoteEntities a JSON-serialized list of special entities that appear in the quote.
	// It can be specified instead of quote_parse_mode.
	//
	// optional
	QuoteEntities []MessageEntity `json:"quote_entities,omitempty"`
	// QuotePosition is a position of the quote in the original message in UTF-16 code units
	//
	// optional
	QuotePosition int `json:"quote_position,omitempty"`
}

const (
	MessageOriginUser       = "user"
	MessageOriginHiddenUser = "hidden_user"
	MessageOriginChat       = "chat"
	MessageOriginChannel    = "channel"
)

// MessageOrigin describes the origin of a message. It can be one of: "user", "hidden_user", "origin_chat", "origin_channel"
type MessageOrigin struct {
	// Type of the message origin.
	Type string `json:"type"`
	// Date the message was sent originally in Unix time
	Date int64 `json:"date"`
	// SenderUser "user" only.
	// Is a user that sent the message originally
	SenderUser *User `json:"sender_user,omitempty"`
	// SenderUserName "hidden_user" only.
	// Name of the user that sent the message originally
	SenderUserName string `json:"sender_user_name,omitempty"`
	// SenderChat "chat" only.
	// Chat that sent the message originally
	SenderChat *Chat `json:"sender_chat,omitempty"`
	// Chat "channel" only.
	// Channel chat to which the message was originally sent
	Chat *Chat `json:"chat,omitempty"`
	// AuthorSignature "chat" and "channel".
	// For "chat": For messages originally sent by an anonymous chat administrator,
	// original message author signature.
	// For "channel": Signature of the original post author
	//
	// Optional
	AuthorSignature string `json:"author_signature,omitempty"`
	// MessageID "channel" only.
	// Unique message identifier inside the chat
	//
	// Optional
	MessageID int `json:"message_id,omitempty"`
}

func (m MessageOrigin) IsUser() bool {
	return m.Type == MessageOriginUser
}

func (m MessageOrigin) IsHiddenUser() bool {
	return m.Type == MessageOriginHiddenUser
}

func (m MessageOrigin) IsChat() bool {
	return m.Type == MessageOriginChat
}

func (m MessageOrigin) IsChannel() bool {
	return m.Type == MessageOriginChannel
}

// PhotoSize represents one size of a photo or a file / sticker thumbnail.
type PhotoSize struct {
	// FileID identifier for this file, which can be used to download or reuse
	// the file
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file, which is supposed to
	// be the same over time and for different bots. Can't be used to download
	// or reuse the file.
	FileUniqueID string `json:"file_unique_id"`
	// Width photo width
	Width int `json:"width"`
	// Height photo height
	Height int `json:"height"`
	// FileSize file size
	//
	// optional
	FileSize int `json:"file_size,omitempty"`
}

// Animation represents an animation file.
type Animation struct {
	// FileID is the identifier for this file, which can be used to download or reuse
	// the file
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file, which is supposed to
	// be the same over time and for different bots. Can't be used to download
	// or reuse the file.
	FileUniqueID string `json:"file_unique_id"`
	// Width video width as defined by sender
	Width int `json:"width"`
	// Height video height as defined by sender
	Height int `json:"height"`
	// Duration of the video in seconds as defined by sender
	Duration int `json:"duration"`
	// Thumbnail animation thumbnail as defined by sender
	//
	// optional
	Thumbnail *PhotoSize `json:"thumbnail,omitempty"`
	// FileName original animation filename as defined by sender
	//
	// optional
	FileName string `json:"file_name,omitempty"`
	// MimeType of the file as defined by sender
	//
	// optional
	MimeType string `json:"mime_type,omitempty"`
	// FileSize file size
	//
	// optional
	FileSize int64 `json:"file_size,omitempty"`
}

// Audio represents an audio file to be treated as music by the Telegram clients.
type Audio struct {
	// FileID is an identifier for this file, which can be used to download or
	// reuse the file
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file, which is supposed to
	// be the same over time and for different bots. Can't be used to download
	// or reuse the file.
	FileUniqueID string `json:"file_unique_id"`
	// Duration of the audio in seconds as defined by sender
	Duration int `json:"duration"`
	// Performer of the audio as defined by sender or by audio tags
	//
	// optional
	Performer string `json:"performer,omitempty"`
	// Title of the audio as defined by sender or by audio tags
	//
	// optional
	Title string `json:"title,omitempty"`
	// FileName is the original filename as defined by sender
	//
	// optional
	FileName string `json:"file_name,omitempty"`
	// MimeType of the file as defined by sender
	//
	// optional
	MimeType string `json:"mime_type,omitempty"`
	// FileSize file size
	//
	// optional
	FileSize int64 `json:"file_size,omitempty"`
	// Thumbnail is the album cover to which the music file belongs
	//
	// optional
	Thumbnail *PhotoSize `json:"thumbnail,omitempty"`
}

// Document represents a general file.
type Document struct {
	// FileID is an identifier for this file, which can be used to download or
	// reuse the file
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file, which is supposed to
	// be the same over time and for different bots. Can't be used to download
	// or reuse the file.
	FileUniqueID string `json:"file_unique_id"`
	// Thumbnail document thumbnail as defined by sender
	//
	// optional
	Thumbnail *PhotoSize `json:"thumbnail,omitempty"`
	// FileName original filename as defined by sender
	//
	// optional
	FileName string `json:"file_name,omitempty"`
	// MimeType  of the file as defined by sender
	//
	// optional
	MimeType string `json:"mime_type,omitempty"`
	// FileSize file size
	//
	// optional
	FileSize int64 `json:"file_size,omitempty"`
}

// Story represents a message about a forwarded story in the chat.
type Story struct {
	// Chat that posted the story
	Chat Chat `json:"chat"`
	// ID is an unique identifier for the story in the chat
	ID int `json:"id"`
}

// Video represents a video file.
type Video struct {
	// FileID identifier for this file, which can be used to download or reuse
	// the file
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file, which is supposed to
	// be the same over time and for different bots. Can't be used to download
	// or reuse the file.
	FileUniqueID string `json:"file_unique_id"`
	// Width video width as defined by sender
	Width int `json:"width"`
	// Height video height as defined by sender
	Height int `json:"height"`
	// Duration of the video in seconds as defined by sender
	Duration int `json:"duration"`
	// Thumbnail video thumbnail
	//
	// optional
	Thumbnail *PhotoSize `json:"thumbnail,omitempty"`
	// FileName is the original filename as defined by sender
	//
	// optional
	FileName string `json:"file_name,omitempty"`
	// MimeType of a file as defined by sender
	//
	// optional
	MimeType string `json:"mime_type,omitempty"`
	// FileSize file size
	//
	// optional
	FileSize int64 `json:"file_size,omitempty"`
}

// VideoNote object represents a video message.
type VideoNote struct {
	// FileID identifier for this file, which can be used to download or reuse the file
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file, which is supposed to
	// be the same over time and for different bots. Can't be used to download
	// or reuse the file.
	FileUniqueID string `json:"file_unique_id"`
	// Length video width and height (diameter of the video message) as defined by sender
	Length int `json:"length"`
	// Duration of the video in seconds as defined by sender
	Duration int `json:"duration"`
	// Thumbnail video thumbnail
	//
	// optional
	Thumbnail *PhotoSize `json:"thumbnail,omitempty"`
	// FileSize file size
	//
	// optional
	FileSize int `json:"file_size,omitempty"`
}

// Voice represents a voice note.
type Voice struct {
	// FileID identifier for this file, which can be used to download or reuse the file
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file, which is supposed to
	// be the same over time and for different bots. Can't be used to download
	// or reuse the file.
	FileUniqueID string `json:"file_unique_id"`
	// Duration of the audio in seconds as defined by sender
	Duration int `json:"duration"`
	// MimeType of the file as defined by sender
	//
	// optional
	MimeType string `json:"mime_type,omitempty"`
	// FileSize file size
	//
	// optional
	FileSize int64 `json:"file_size,omitempty"`
}

// PaidMediaInfo describes the paid media added to a message.
type PaidMediaInfo struct {
	// The number of Telegram Stars that must be paid to buy access to the media
	StarCount int64 `json:"star_count"`
	// Information about the paid media
	PaidMedia []PaidMedia `json:"paid_media"`
}

// This object describes paid media. Currently, it can be one of
//   - PaidMediaPreview
//   - PaidMediaPhoto
//   - PaidMediaVideo
type PaidMedia struct {
	// Type of the paid media, should be one of:
	//   - "photo"
	//   - "video"
	//   - "preview"
	Type string `json:"type"`
	// PaidMediaPreview only.
	// Media width as defined by the sender.
	//
	// optional
	Width int64 `json:"width,omitempty"`
	// PaidMediaPreview only.
	// Media height as defined by the sender
	//
	// optional
	Height int64 `json:"height,omitempty"`
	// PaidMediaPreview only.
	// Duration of the media in seconds as defined by the sender
	//
	// optional
	Duration int64 `json:"duration,omitempty"`
	// PaidMediaPhoto only.
	// The photo
	Photo []PhotoSize `json:"photo,omitempty"`
	// PaidMediaVideo only.
	// The video
	Video *Video `json:"video,omitempty"`
}

// Contact represents a phone contact.
//
// Note that LastName and UserID may be empty.
type Contact struct {
	// PhoneNumber contact's phone number
	PhoneNumber string `json:"phone_number"`
	// FirstName contact's first name
	FirstName string `json:"first_name"`
	// LastName contact's last name
	//
	// optional
	LastName string `json:"last_name,omitempty"`
	// UserID contact's user identifier in Telegram
	//
	// optional
	UserID int64 `json:"user_id,omitempty"`
	// VCard is additional data about the contact in the form of a vCard.
	//
	// optional
	VCard string `json:"vcard,omitempty"`
}

// Dice represents an animated emoji that displays a random value.
type Dice struct {
	// Emoji on which the dice throw animation is based
	Emoji string `json:"emoji"`
	// Value of the dice
	Value int `json:"value"`
}

// PollOption contains information about one answer option in a poll.
type PollOption struct {
	// Text is the option text, 1-100 characters
	Text string `json:"text"`
	// Special entities that appear in the option text.
	// Currently, only custom emoji entities are allowed in poll option texts
	//
	// optional
	TextEntities []MessageEntity `json:"text_entities,omitempty"`
	// VoterCount is the number of users that voted for this option
	VoterCount int `json:"voter_count"`
}

// InputPollOption contains information about one answer option in a poll to send.
type InputPollOption struct {
	// Option text, 1-100 characters
	Text string `json:"text"`
	// Mode for parsing entities in the text. See formatting options for more details.
	// Currently, only custom emoji entities are allowed
	//
	// optional
	TextParseMode string `json:"text_parse_mode,omitempty"`
	// A JSON-serialized list of special entities that appear in the poll option text.
	// It can be specified instead of text_parse_mode
	//
	// optional
	TextEntities []MessageEntity `json:"text_entities,omitempty"`
}

// PollAnswer represents an answer of a user in a non-anonymous poll.
type PollAnswer struct {
	// PollID is the unique poll identifier
	PollID string `json:"poll_id"`
	// Chat that changed the answer to the poll, if the voter is anonymous.
	//
	// Optional
	VoterChat *Chat `json:"voter_chat,omitempty"`
	// User who changed the answer to the poll, if the voter isn't anonymous
	// For backward compatibility, the field user in such objects
	// will contain the user 136817688 (@Channel_Bot).
	//
	// Optional
	User *User `json:"user,omitempty"`
	// OptionIDs is the 0-based identifiers of poll options chosen by the user.
	// May be empty if user retracted vote.
	OptionIDs []int `json:"option_ids"`
}

// Poll contains information about a poll.
type Poll struct {
	// ID is the unique poll identifier
	ID string `json:"id"`
	// Question is the poll question, 1-255 characters
	Question string `json:"question"`
	// Special entities that appear in the question.
	// Currently, only custom emoji entities are allowed in poll questions
	//
	// optional
	QuestionEntities []MessageEntity `json:"question_entities,omitempty"`
	// Options is the list of poll options
	Options []PollOption `json:"options"`
	// TotalVoterCount is the total numbers of users who voted in the poll
	TotalVoterCount int `json:"total_voter_count"`
	// IsClosed is if the poll is closed
	IsClosed bool `json:"is_closed"`
	// IsAnonymous is if the poll is anonymous
	IsAnonymous bool `json:"is_anonymous"`
	// Type is the poll type, currently can be "regular" or "quiz"
	Type string `json:"type"`
	// AllowsMultipleAnswers is true, if the poll allows multiple answers
	AllowsMultipleAnswers bool `json:"allows_multiple_answers"`
	// CorrectOptionID is the 0-based identifier of the correct answer option.
	// Available only for polls in quiz mode, which are closed, or was sent (not
	// forwarded) by the bot or to the private chat with the bot.
	//
	// optional
	CorrectOptionID int `json:"correct_option_id,omitempty"`
	// Explanation is text that is shown when a user chooses an incorrect answer
	// or taps on the lamp icon in a quiz-style poll, 0-200 characters
	//
	// optional
	Explanation string `json:"explanation,omitempty"`
	// ExplanationEntities are special entities like usernames, URLs, bot
	// commands, etc. that appear in the explanation
	//
	// optional
	ExplanationEntities []MessageEntity `json:"explanation_entities,omitempty"`
	// OpenPeriod is the amount of time in seconds the poll will be active
	// after creation
	//
	// optional
	OpenPeriod int `json:"open_period,omitempty"`
	// CloseDate is the point in time (unix timestamp) when the poll will be
	// automatically closed
	//
	// optional
	CloseDate int `json:"close_date,omitempty"`
}

// Location represents a point on the map.
type Location struct {
	// Longitude as defined by sender
	Longitude float64 `json:"longitude"`
	// Latitude as defined by sender
	Latitude float64 `json:"latitude"`
	// HorizontalAccuracy is the radius of uncertainty for the location,
	// measured in meters; 0-1500
	//
	// optional
	HorizontalAccuracy float64 `json:"horizontal_accuracy,omitempty"`
	// LivePeriod is time relative to the message sending date, during which the
	// location can be updated, in seconds. For active live locations only.
	// Use 0x7FFFFFFF (2147483647 - max positive Int) to edit indefinitely
	//
	// optional
	LivePeriod int `json:"live_period,omitempty"`
	// Heading is the direction in which user is moving, in degrees; 1-360. For
	// active live locations only.
	//
	// optional
	Heading int `json:"heading,omitempty"`
	// ProximityAlertRadius is the maximum distance for proximity alerts about
	// approaching another chat member, in meters. For sent live locations only.
	//
	// optional
	ProximityAlertRadius int `json:"proximity_alert_radius,omitempty"`
}

// Venue represents a venue.
type Venue struct {
	// Location is the venue location
	Location Location `json:"location"`
	// Title is the name of the venue
	Title string `json:"title"`
	// Address of the venue
	Address string `json:"address"`
	// FoursquareID is the foursquare identifier of the venue
	//
	// optional
	FoursquareID string `json:"foursquare_id,omitempty"`
	// FoursquareType is the foursquare type of the venue
	//
	// optional
	FoursquareType string `json:"foursquare_type,omitempty"`
	// GooglePlaceID is the Google Places identifier of the venue
	//
	// optional
	GooglePlaceID string `json:"google_place_id,omitempty"`
	// GooglePlaceType is the Google Places type of the venue
	//
	// optional
	GooglePlaceType string `json:"google_place_type,omitempty"`
}

// WebAppData Contains data sent from a Web App to the bot.
type WebAppData struct {
	// Data is the data. Be aware that a bad client can send arbitrary data in this field.
	Data string `json:"data"`
	// ButtonText is the text of the web_app keyboard button, from which the Web App
	// was opened. Be aware that a bad client can send arbitrary data in this field.
	ButtonText string `json:"button_text"`
}

// ProximityAlertTriggered represents a service message sent when a user in the
// chat triggers a proximity alert sent by another user.
type ProximityAlertTriggered struct {
	// Traveler is the user that triggered the alert
	Traveler User `json:"traveler"`
	// Watcher is the user that set the alert
	Watcher User `json:"watcher"`
	// Distance is the distance between the users
	Distance int `json:"distance"`
}

// MessageAutoDeleteTimerChanged represents a service message about a change in
// auto-delete timer settings.
type MessageAutoDeleteTimerChanged struct {
	// New auto-delete time for messages in the chat.
	MessageAutoDeleteTime int `json:"message_auto_delete_time"`
}

// ChatBoostAdded represents a service message about a user boosting a chat.
type ChatBoostAdded struct {
	// BoostCount is a number of boosts added by the user
	BoostCount int `json:"boost_count"`
}

// BackgroundFill describes the way a background is filled based on the selected colors.
// Currently, it can be one of:
//   - BackgroundFillSolid
//   - BackgroundFillGradient
//   - BackgroundFillFreeformGradient
type BackgroundFill struct {
	// Type of the background fill, can be:
	//  - solid
	//  - gradient
	//  - freeform_gradient
	Type string `json:"type"`
	// BackgroundFillSolid only.
	// The color of the background fill in the RGB24 format
	Color int `json:"color"`
	// BackgroundFillGradient only.
	// Top color of the gradient in the RGB24 format
	TopColor int `json:"top_color"`
	// BackgroundFillGradient only.
	// Bottom color of the gradient in the RGB24 format
	BottomColor int `json:"bottom_color"`
	// BackgroundFillGradient only.
	// Clockwise rotation angle of the background fill in degrees; 0-359
	RotationAngle int `json:"rotation_angle"`
	// BackgroundFillFreeformGradient only.
	// A list of the 3 or 4 base colors that are used to generate the freeform gradient in the RGB24 format
	Colors []int `json:"colors"`
}

// BackgroundType describes the type of a background. Currently, it can be one of:
//   - BackgroundTypeFill
//   - BackgroundTypeWallpaper
//   - BackgroundTypePattern
//   - BackgroundTypeChatTheme
type BackgroundType struct {
	// Type of the background.
	// Currently, it can be one of:
	//  - fill
	//  - wallpaper
	//  - pattern
	//  - chat_theme
	Type string `json:"type"`
	// BackgroundTypeFill and BackgroundTypePattern only.
	// The background fill or fill that is combined with the pattern
	Fill BackgroundFill `json:"fill"`
	// BackgroundTypeFill and BackgroundTypeWallpaper only.
	// Dimming of the background in dark themes, as a percentage; 0-100
	DarkThemeDimming int `json:"dark_theme_dimming"`
	// BackgroundTypeWallpaper and BackgroundTypePattern only.
	// Document with the wallpaper / pattern
	Document Document `json:"document"`
	// BackgroundTypeWallpaper only.
	// True, if the wallpaper is downscaled to fit in a 450x450 square and then box-blurred with radius 12
	//
	// optional
	IsBlurred bool `json:"is_blurred,omitempty"`
	// BackgroundTypeWallpaper and BackgroundTypePattern only.
	// True, if the background moves slightly when the device is tilted
	//
	// optional
	IsMoving bool `json:"is_moving,omitempty"`
	// BackgroundTypePattern only.
	// Intensity of the pattern when it is shown above the filled background; 0-100
	Intensity int `json:"intensity"`
	// BackgroundTypePattern only.
	// True, if the background fill must be applied only to the pattern itself.
	// All other pixels are black in this case. For dark themes only
	//
	// optional
	IsInverted bool `json:"is_inverted,omitempty"`
	// BackgroundTypeChatTheme only.
	// Name of the chat theme, which is usually an emoji
	ThemeName string `json:"theme_name"`
}

// ChatBackground represents a chat background.
type ChatBackground struct {
	// Type of the background
	Type BackgroundType `json:"type"`
}

// ForumTopicCreated represents a service message about a new forum topic
// created in the chat.
type ForumTopicCreated struct {
	// Name is the name of topic
	Name string `json:"name"`
	// IconColor is the color of the topic icon in RGB format
	IconColor int `json:"icon_color"`
	// IconCustomEmojiID is the unique identifier of the custom emoji
	// shown as the topic icon
	//
	// optional
	IconCustomEmojiID string `json:"icon_custom_emoji_id,omitempty"`
}

// ForumTopicClosed represents a service message about a forum topic
// closed in the chat. Currently holds no information.
type ForumTopicClosed struct {
}

// ForumTopicEdited object represents a service message about an edited forum topic.
type ForumTopicEdited struct {
	// Name is the new name of the topic, if it was edited
	//
	// optional
	Name string `json:"name,omitempty"`
	// IconCustomEmojiID is the new identifier of the custom emoji
	// shown as the topic icon, if it was edited;
	// an empty string if the icon was removed
	//
	// optional
	IconCustomEmojiID *string `json:"icon_custom_emoji_id,omitempty"`
}

// ForumTopicReopened represents a service message about a forum topic
// reopened in the chat. Currently holds no information.
type ForumTopicReopened struct {
}

// GeneralForumTopicHidden represents a service message about General forum topic
// hidden in the chat. Currently holds no information.
type GeneralForumTopicHidden struct {
}

// GeneralForumTopicUnhidden represents a service message about General forum topic
// unhidden in the chat. Currently holds no information.
type GeneralForumTopicUnhidden struct {
}

// SharedUser contains information about a user that was
// shared with the bot using a KeyboardButtonRequestUsers button.
type SharedUser struct {
	// UserID is the identifier of the shared user.
	UserID int64 `json:"user_id"`
	// FirstName of the user, if the name was requested by the bot.
	//
	// optional
	FirstName *string `json:"first_name,omitempty"`
	// LastName of the user, if the name was requested by the bot.
	//
	// optional
	LastName *string `json:"last_name,omitempty"`
	// Username of the user, if the username was requested by the bot.
	//
	// optional
	UserName *string `json:"username,omitempty"`
	// Photo is array of available sizes of the chat photo,
	// if the photo was requested by the bot
	//
	// optional
	Photo []PhotoSize `json:"photo,omitempty"`
}

// UsersShared object contains information about the user whose identifier
// was shared with the bot using a KeyboardButtonRequestUser button.
type UsersShared struct {
	// RequestID is an indentifier of the request.
	RequestID int `json:"request_id"`
	// Users shared with the bot.
	Users []SharedUser `json:"users"`
}

// ChatShared contains information about the chat whose identifier
// was shared with the bot using a KeyboardButtonRequestChat button.
type ChatShared struct {
	// RequestID is an indentifier of the request.
	RequestID int `json:"request_id"`
	// ChatID is an identifier of the shared chat.
	ChatID int64 `json:"chat_id"`
	// Title of the chat, if the title was requested by the bot.
	//
	// optional
	Title *string `json:"title,omitempty"`
	// UserName of the chat, if the username was requested by
	// the bot and available.
	//
	// optional
	UserName *string `json:"username,omitempty"`
	// Photo is array of available sizes of the chat photo,
	// if the photo was requested by the bot
	//
	// optional
	Photo []PhotoSize `json:"photo,omitempty"`
}

// WriteAccessAllowed represents a service message about a user allowing a bot
// to write messages after adding the bot to the attachment menu or launching
// a Web App from a link.
type WriteAccessAllowed struct {
	// FromRequest is true, if the access was granted after
	// the user accepted an explicit request from a Web App
	// sent by the method requestWriteAccess.
	//
	// Optional
	FromRequest bool `json:"from_request,omitempty"`
	// Name of the Web App which was launched from a link
	//
	// Optional
	WebAppName string `json:"web_app_name,omitempty"`
	// FromAttachmentMenu is true, if the access was granted when
	// the bot was added to the attachment or side menu
	//
	// Optional
	FromAttachmentMenu bool `json:"from_attachment_menu,omitempty"`
}

// VideoChatScheduled represents a service message about a voice chat scheduled
// in the chat.
type VideoChatScheduled struct {
	// Point in time (Unix timestamp) when the voice chat is supposed to be
	// started by a chat administrator
	StartDate int `json:"start_date"`
}

// Time converts the scheduled start date into a Time.
func (m *VideoChatScheduled) Time() time.Time {
	return time.Unix(int64(m.StartDate), 0)
}

// VideoChatStarted represents a service message about a voice chat started in
// the chat.
type VideoChatStarted struct{}

// VideoChatEnded represents a service message about a voice chat ended in the
// chat.
type VideoChatEnded struct {
	// Voice chat duration; in seconds.
	Duration int `json:"duration"`
}

// VideoChatParticipantsInvited represents a service message about new members
// invited to a voice chat.
type VideoChatParticipantsInvited struct {
	// New members that were invited to the voice chat.
	//
	// optional
	Users []User `json:"users,omitempty"`
}

// This object represents a service message about the creation of a scheduled giveaway. Currently holds no information.
type GiveawayCreated struct {
	// PrizeStarCount is the number of Telegram Stars to be split
	// between giveaway winners;
	// for Telegram Star giveaways only
	//
	// optional
	PrizeStarCount int `json:"prize_star_count,omitempty"`
}

// Giveaway represents a message about a scheduled giveaway.
type Giveaway struct {
	// Chats is the list of chats which the user must join to participate in the giveaway
	Chats []Chat `json:"chats"`
	// WinnersSelectionDate is point in time (Unix timestamp) when
	// winners of the giveaway will be selected
	WinnersSelectionDate int64 `json:"winners_selection_date"`
	// WinnerCount is the number of users which are supposed
	// to be selected as winners of the giveaway
	WinnerCount int `json:"winner_count"`
	// OnlyNewMembers True, if only users who join the chats after
	// the giveaway started should be eligible to win
	//
	// optional
	OnlyNewMembers bool `json:"only_new_members,omitempty"`
	// HasPublicWinners True, if the list of giveaway winners will be visible to everyone
	//
	// optional
	HasPublicWinners bool `json:"has_public_winners,omitempty"`
	// PrizeDescription is description of additional giveaway prize
	//
	// optional
	PrizeDescription string `json:"prize_description,omitempty"`
	// CountryCodes is a list of two-letter ISO 3166-1 alpha-2 country codes
	// indicating the countries from which eligible users for the giveaway must come.
	// If empty, then all users can participate in the giveaway.
	//
	// optional
	CountryCodes []string `json:"country_codes,omitempty"`
	// PrizeStarCount is the number of Telegram Stars to be split
	// between giveaway winners;
	// for Telegram Star giveaways only
	//
	// optional
	PrizeStarCount int `json:"prize_star_count,omitempty"`
	// PremiumSubscriptionMonthCount the number of months the Telegram Premium
	// subscription won from the giveaway will be active for
	//
	// optional
	PremiumSubscriptionMonthCount int `json:"premium_subscription_month_count,omitempty"`
}

// Giveaway represents a message about a scheduled giveaway.
type GiveawayWinners struct {
	// Chat that created the giveaway
	Chat Chat `json:"chat"`
	// GiveawayMessageID is the identifier of the messsage with the giveaway in the chat
	GiveawayMessageID int `json:"giveaway_message_id"`
	// WinnersSelectionDate is point in time (Unix timestamp) when
	// winners of the giveaway will be selected
	WinnersSelectionDate int64 `json:"winners_selection_date"`
	// WinnerCount is the number of users which are supposed
	// to be selected as winners of the giveaway
	WinnerCount int `json:"winner_count"`
	// Winners is a list of up to 100 winners of the giveaway
	Winners []User `json:"winners"`
	// AdditionalChatCount is the number of other chats
	// the user had to join in order to be eligible for the giveaway
	//
	// optional
	AdditionalChatCount int `json:"additional_chat_count,omitempty"`
	// PrizeStarCount is the number of Telegram Stars to be split
	// between giveaway winners;
	// for Telegram Star giveaways only
	//
	// optional
	PrizeStarCount int `json:"prize_star_count,omitempty"`
	// PremiumSubscriptionMonthCount the number of months the Telegram Premium
	// subscription won from the giveaway will be active for
	//
	// optional
	PremiumSubscriptionMonthCount int `json:"premium_subscription_month_count,omitempty"`
	// UnclaimedPrizeCount is the number of undistributed prizes
	//
	// optional
	UnclaimedPrizeCount int `json:"unclaimed_prize_count,omitempty"`
	// OnlyNewMembers True, if only users who join the chats after
	// the giveaway started should be eligible to win
	//
	// optional
	OnlyNewMembers bool `json:"only_new_members,omitempty"`
	// WasRefunded True, if the giveaway was canceled because the payment for it was refunded
	//
	// optional
	WasRefunded bool `json:"was_refunded,omitempty"`
	// PrizeDescription is description of additional giveaway prize
	//
	// optional
	PrizeDescription string `json:"prize_description,omitempty"`
}

// This object represents a service message about the completion of a giveaway without public winners.
type GiveawayCompleted struct {
	// Number of winners in the giveaway
	WinnerCount int `json:"winner_count"`
	// Number of undistributed prizes
	//
	// optional
	UnclaimedPrizeCount int `json:"unclaimed_prize_count,omitempty"`
	// Message with the giveaway that was completed, if it wasn't deleted
	//
	// optional
	GiveawayMessage *Message `json:"giveaway_message,omitempty"`
	// IsStarGiveaway True, if the giveaway is a Telegram Star giveaway.
	// Otherwise, currently, the giveaway is a Telegram Premium giveaway.
	//
	// optional
	IsStarGiveaway bool `json:"is_star_giveaway,omitempty"`
}

// LinkPreviewOptions describes the options used for link preview generation.
type LinkPreviewOptions struct {
	// IsDisabled True, if the link preview is disabled
	//
	// optional
	IsDisabled bool `json:"is_disabled,omitempty"`
	// URL to use for the link preview. If empty,
	// then the first URL found in the message text will be used
	//
	// optional
	URL string `json:"url,omitempty"`
	// PreferSmallMedia True, if the media in the link preview is suppposed
	//  to be shrunk; ignored if the URL isn't explicitly specified
	// or media size change isn't supported for the preview
	//
	// optional
	PreferSmallMedia bool `json:"prefer_small_media,omitempty"`
	// PreferLargeMedia True, if the media in the link preview is suppposed
	// to be enlarged; ignored if the URL isn't explicitly specified
	// or media size change isn't supported for the preview
	//
	// optional
	PreferLargeMedia bool `json:"prefer_large_media,omitempty"`
	// ShowAboveText True, if the link preview must be shown above the message text;
	// otherwise, the link preview will be shown below the message text
	//
	// optional
	ShowAboveText bool `json:"show_above_text,omitempty"`
}

// UserProfilePhotos contains a set of user profile photos.
type UserProfilePhotos struct {
	// TotalCount total number of profile pictures the target user has
	TotalCount int `json:"total_count"`
	// Photos requested profile pictures (in up to 4 sizes each)
	Photos [][]PhotoSize `json:"photos"`
}

// File contains information about a file to download from Telegram.
type File struct {
	// FileID identifier for this file, which can be used to download or reuse
	// the file
	FileID string `json:"file_id"`
	// FileUniqueID is the unique identifier for this file, which is supposed to
	// be the same over time and for different bots. Can't be used to download
	// or reuse the file.
	FileUniqueID string `json:"file_unique_id"`
	// FileSize file size, if known
	//
	// optional
	FileSize int64 `json:"file_size,omitempty"`
	// FilePath file path
	//
	// optional
	FilePath string `json:"file_path,omitempty"`
}

// Link returns a full path to the download URL for a File.
//
// It requires the Bot token to create the link.
func (f *File) Link(token string) string {
	return fmt.Sprintf(FileEndpoint, token, f.FilePath)
}

// WebAppInfo contains information about a Web App.
type WebAppInfo struct {
	// URL is the HTTPS URL of a Web App to be opened with additional data as
	// specified in Initializing Web Apps.
	URL string `json:"url"`
}

// ReplyKeyboardMarkup represents a custom keyboard with reply options.
type ReplyKeyboardMarkup struct {
	// Keyboard is an array of button rows, each represented by an Array of KeyboardButton objects
	Keyboard [][]KeyboardButton `json:"keyboard"`
	// IsPersistent requests clients to always show the keyboard
	// when the regular keyboard is hidden.
	// Defaults to false, in which case the custom keyboard can be hidden
	// and opened with a keyboard icon.
	//
	// optional
	IsPersistent bool `json:"is_persistent"`
	// ResizeKeyboard requests clients to resize the keyboard vertically for optimal fit
	// (e.g., make the keyboard smaller if there are just two rows of buttons).
	// Defaults to false, in which case the custom keyboard
	// is always of the same height as the app's standard keyboard.
	//
	// optional
	ResizeKeyboard bool `json:"resize_keyboard,omitempty"`
	// OneTimeKeyboard requests clients to hide the keyboard as soon as it's been used.
	// The keyboard will still be available, but clients will automatically display
	// the usual letter-keyboard in the chat – the user can press a special button
	// in the input field to see the custom keyboard again.
	// Defaults to false.
	//
	// optional
	OneTimeKeyboard bool `json:"one_time_keyboard,omitempty"`
	// InputFieldPlaceholder is the placeholder to be shown in the input field when
	// the keyboard is active; 1-64 characters.
	//
	// optional
	InputFieldPlaceholder string `json:"input_field_placeholder,omitempty"`
	// Selective use this parameter if you want to show the keyboard to specific users only.
	// Targets:
	//  1) users that are @mentioned in the text of the Message object;
	//  2) if the bot's message is a reply (has Message.ReplyToMessage not nil), sender of the original message.
	//
	// Example: A user requests to change the bot's language,
	// bot replies to the request with a keyboard to select the new language.
	// Other users in the group don't see the keyboard.
	//
	// optional
	Selective bool `json:"selective,omitempty"`
}

// KeyboardButton represents one button of the reply keyboard. For simple text
// buttons String can be used instead of this object to specify text of the
// button. Optional fields request_contact, request_location, and request_poll
// are mutually exclusive.
type KeyboardButton struct {
	// Text of the button. If none of the optional fields are used,
	// it will be sent as a message when the button is pressed.
	Text string `json:"text"`
	// RequestUsers if specified, pressing the button will open
	// a list of suitable users. Tapping on any user will send
	// their identifier to the bot in a "user_shared" service message.
	// Available in private chats only.
	//
	// optional
	RequestUsers *KeyboardButtonRequestUsers `json:"request_users,omitempty"`
	// RequestChat if specified, pressing the button will open
	// a list of suitable chats. Tapping on a chat will send
	// its identifier to the bot in a "chat_shared" service message.
	// Available in private chats only.
	//
	// optional
	RequestChat *KeyboardButtonRequestChat `json:"request_chat,omitempty"`
	// RequestContact if True, the user's phone number will be sent
	// as a contact when the button is pressed.
	// Available in private chats only.
	//
	// optional
	RequestContact bool `json:"request_contact,omitempty"`
	// RequestLocation if True, the user's current location will be sent when
	// the button is pressed.
	// Available in private chats only.
	//
	// optional
	RequestLocation bool `json:"request_location,omitempty"`
	// RequestPoll if specified, the user will be asked to create a poll and send it
	// to the bot when the button is pressed. Available in private chats only
	//
	// optional
	RequestPoll *KeyboardButtonPollType `json:"request_poll,omitempty"`
	// WebApp if specified, the described Web App will be launched when the button
	// is pressed. The Web App will be able to send a “web_app_data” service
	// message. Available in private chats only.
	//
	// optional
	WebApp *WebAppInfo `json:"web_app,omitempty"`
}

// KeyboardButtonRequestUsers defines the criteria used to request
// a suitable user. The identifier of the selected user will be shared
// with the bot when the corresponding button is pressed.
type KeyboardButtonRequestUsers struct {
	// RequestID is a signed 32-bit identifier of the request.
	RequestID int `json:"request_id"`
	// UserIsBot pass True to request a bot,
	// pass False to request a regular user.
	// If not specified, no additional restrictions are applied.
	//
	// optional
	UserIsBot *bool `json:"user_is_bot,omitempty"`
	// UserIsPremium pass True to request a premium user,
	// pass False to request a non-premium user.
	// If not specified, no additional restrictions are applied.
	//
	// optional
	UserIsPremium *bool `json:"user_is_premium,omitempty"`
	// MaxQuantity is the maximum number of users to be selected.
	// 1-10. Defaults to 1
	//
	// optional
	MaxQuantity int `json:"max_quantity,omitempty"`
	// RequestName pass True to request the users' first and last names
	//
	// optional
	RequestName bool `json:"request_name,omitempty"`
	// RequestUsername pass True to request the users' usernames
	//
	// optional
	RequestUsername bool `json:"request_username,omitempty"`
	// RequestPhoto pass True to request the users' photos
	//
	// optional
	RequestPhoto bool `json:"request_photo,omitempty"`
}

// KeyboardButtonRequestChat defines the criteria used to request
// a suitable chat. The identifier of the selected chat will be shared
// with the bot when the corresponding button is pressed.
type KeyboardButtonRequestChat struct {
	// RequestID is a signed 32-bit identifier of the request.
	RequestID int `json:"request_id"`
	// ChatIsChannel pass True to request a channel chat,
	// pass False to request a group or a supergroup chat.
	ChatIsChannel bool `json:"chat_is_channel"`
	// ChatIsForum pass True to request a forum supergroup,
	// pass False to request a non-forum chat.
	// If not specified, no additional restrictions are applied.
	//
	// optional
	ChatIsForum bool `json:"chat_is_forum,omitempty"`
	// ChatHasUsername pass True to request a supergroup or a channel with a username,
	// pass False to request a chat without a username.
	// If not specified, no additional restrictions are applied.
	//
	// optional
	ChatHasUsername bool `json:"chat_has_username,omitempty"`
	// ChatIsCreated pass True to request a chat owned by the user.
	// Otherwise, no additional restrictions are applied.
	//
	// optional
	ChatIsCreated bool `json:"chat_is_created,omitempty"`
	// UserAdministratorRights is a JSON-serialized object listing
	// the required administrator rights of the user in the chat.
	// If not specified, no additional restrictions are applied.
	//
	// optional
	UserAdministratorRights *ChatAdministratorRights `json:"user_administrator_rights,omitempty"`
	// BotAdministratorRights is a JSON-serialized object listing
	// the required administrator rights of the bot in the chat.
	// The rights must be a subset of user_administrator_rights.
	// If not specified, no additional restrictions are applied.
	//
	// optional
	BotAdministratorRights *ChatAdministratorRights `json:"bot_administrator_rights,omitempty"`
	// BotIsMember pass True to request a chat with the bot as a member.
	// Otherwise, no additional restrictions are applied.
	//
	// optional
	BotIsMember bool `json:"bot_is_member,omitempty"`
	// RequestTitle pass True to request the chat's title
	//
	// optional
	RequestTitle bool `json:"request_title,omitempty"`
	// RequestUsername pass True to request the chat's username
	//
	// optional
	RequestUsername bool `json:"request_username,omitempty"`
	// RequestPhoto pass True to request the chat's photo
	//
	// optional
	RequestPhoto bool `json:"request_photo,omitempty"`
}

// KeyboardButtonPollType represents type of poll, which is allowed to
// be created and sent when the corresponding button is pressed.
type KeyboardButtonPollType struct {
	// Type is if quiz is passed, the user will be allowed to create only polls
	// in the quiz mode. If regular is passed, only regular polls will be
	// allowed. Otherwise, the user will be allowed to create a poll of any type.
	Type string `json:"type"`
}

// ReplyKeyboardRemove Upon receiving a message with this object, Telegram
// clients will remove the current custom keyboard and display the default
// letter-keyboard. By default, custom keyboards are displayed until a new
// keyboard is sent by a bot. An exception is made for one-time keyboards
// that are hidden immediately after the user presses a button.
type ReplyKeyboardRemove struct {
	// RemoveKeyboard requests clients to remove the custom keyboard
	// (user will not be able to summon this keyboard;
	// if you want to hide the keyboard from sight but keep it accessible,
	// use one_time_keyboard in ReplyKeyboardMarkup).
	RemoveKeyboard bool `json:"remove_keyboard"`
	// Selective use this parameter if you want to remove the keyboard for specific users only.
	// Targets:
	//  1) users that are @mentioned in the text of the Message object;
	//  2) if the bot's message is a reply (has Message.ReplyToMessage not nil), sender of the original message.
	//
	// Example: A user votes in a poll, bot returns confirmation message
	// in reply to the vote and removes the keyboard for that user,
	// while still showing the keyboard with poll options to users who haven't voted yet.
	//
	// optional
	Selective bool `json:"selective,omitempty"`
}

// InlineKeyboardMarkup represents an inline keyboard that appears right next to
// the message it belongs to.
type InlineKeyboardMarkup struct {
	// InlineKeyboard array of button rows, each represented by an Array of
	// InlineKeyboardButton objects
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// InlineKeyboardButton represents one button of an inline keyboard. You must
// use exactly one of the optional fields.
//
// Note that some values are references as even an empty string
// will change behavior.
//
// CallbackGame, if set, MUST be first button in first row.
type InlineKeyboardButton struct {
	// Text label text on the button
	Text string `json:"text"`
	// URL HTTP or tg:// url to be opened when button is pressed.
	// Links tg://user?id=<user_id> can be used to mention a user by their identifier without using a username,
	// if this is allowed by their privacy settings.
	//
	// optional
	URL *string `json:"url,omitempty"`
	// LoginURL is an HTTP URL used to automatically authorize the user. Can be
	// used as a replacement for the Telegram Login Widget
	//
	// optional
	LoginURL *LoginURL `json:"login_url,omitempty"`
	// CallbackData data to be sent in a callback query to the bot when button is pressed, 1-64 bytes.
	//
	// optional
	CallbackData *string `json:"callback_data,omitempty"`
	// WebApp is the Description of the Web App that will be launched when the user presses the button.
	// The Web App will be able to send an arbitrary message on behalf of the user using the method
	// answerWebAppQuery. Available only in private chats between a user and the bot.
	// Not supported for messages sent on behalf of a Telegram Business account.
	//
	// optional
	WebApp *WebAppInfo `json:"web_app,omitempty"`
	// SwitchInlineQuery if set, pressing the button will prompt the user to select one of their chats,
	// open that chat and insert the bot's username and the specified inline query in the input field.
	// Can be empty, in which case just the bot's username will be inserted.
	//
	// This offers an easy way for users to start using your bot
	// in inline mode when they are currently in a private chat with it.
	// Especially useful when combined with switch_pm… actions – in this case
	// the user will be automatically returned to the chat they switched from,
	// skipping the chat selection screen.
	// Not supported for messages sent on behalf of a Telegram Business account.
	//
	// optional
	SwitchInlineQuery *string `json:"switch_inline_query,omitempty"`
	// SwitchInlineQueryCurrentChat if set, pressing the button will insert the bot's username
	// and the specified inline query in the current chat's input field.
	// Can be empty, in which case only the bot's username will be inserted.
	//
	// This offers a quick way for the user to open your bot in inline mode
	// in the same chat – good for selecting something from multiple options.
	// Not supported for messages sent on behalf of a Telegram Business account.
	//
	// optional
	SwitchInlineQueryCurrentChat *string `json:"switch_inline_query_current_chat,omitempty"`
	//SwitchInlineQueryChosenChat If set, pressing the button will prompt the user to
	//select one of their chats of the specified type, open that chat and insert the bot's
	//username and the specified inline query in the input field.
	// Not supported for messages sent on behalf of a Telegram Business account.
	//
	//optional
	SwitchInlineQueryChosenChat *SwitchInlineQueryChosenChat `json:"switch_inline_query_chosen_chat,omitempty"`
	// CallbackGame description of the game that will be launched when the user presses the button.
	//
	// optional
	CallbackGame *CallbackGame `json:"callback_game,omitempty"`
	// Pay specify True, to send a Pay button.
	// Substrings “⭐” and “XTR” in the buttons's text will be replaced with a Telegram Star icon.
	//
	// NOTE: This type of button must always be the first button in the first row.
	//
	// optional
	Pay bool `json:"pay,omitempty"`
}

// LoginURL represents a parameter of the inline keyboard button used to
// automatically authorize a user. Serves as a great replacement for the
// Telegram Login Widget when the user is coming from Telegram. All the user
// needs to do is tap/click a button and confirm that they want to log in.
type LoginURL struct {
	// URL is an HTTP URL to be opened with user authorization data added to the
	// query string when the button is pressed. If the user refuses to provide
	// authorization data, the original URL without information about the user
	// will be opened. The data added is the same as described in Receiving
	// authorization data.
	//
	// NOTE: You must always check the hash of the received data to verify the
	// authentication and the integrity of the data as described in Checking
	// authorization.
	URL string `json:"url"`
	// ForwardText is the new text of the button in forwarded messages
	//
	// optional
	ForwardText string `json:"forward_text,omitempty"`
	// BotUsername is the username of a bot, which will be used for user
	// authorization. See Setting up a bot for more details. If not specified,
	// the current bot's username will be assumed. The url's domain must be the
	// same as the domain linked with the bot. See Linking your domain to the
	// bot for more details.
	//
	// optional
	BotUsername string `json:"bot_username,omitempty"`
	// RequestWriteAccess if true requests permission for your bot to send
	// messages to the user
	//
	// optional
	RequestWriteAccess bool `json:"request_write_access,omitempty"`
}

// CallbackQuery represents an incoming callback query from a callback button in
// an inline keyboard. If the button that originated the query was attached to a
// message sent by the bot, the field message will be present. If the button was
// attached to a message sent via the bot (in inline mode), the field
// inline_message_id will be present. Exactly one of the fields data or
// game_short_name will be present.
type CallbackQuery struct {
	// ID unique identifier for this query
	ID string `json:"id"`
	// From sender
	From *User `json:"from"`
	// Message sent by the bot with the callback button that originated the query
	//
	// optional
	Message *Message `json:"message,omitempty"`
	// InlineMessageID identifier of the message sent via the bot in inline
	// mode, that originated the query.
	//
	// optional
	InlineMessageID string `json:"inline_message_id,omitempty"`
	// ChatInstance global identifier, uniquely corresponding to the chat to
	// which the message with the callback button was sent. Useful for high
	// scores in games.
	ChatInstance string `json:"chat_instance"`
	// Data associated with the callback button. Be aware that
	// a bad client can send arbitrary data in this field.
	//
	// optional
	Data string `json:"data,omitempty"`
	// GameShortName short name of a Game to be returned, serves as the unique identifier for the game.
	//
	// optional
	GameShortName string `json:"game_short_name,omitempty"`
}

// IsInaccessibleMessage method that shows whether message is inaccessible
func (c CallbackQuery) IsInaccessibleMessage() bool {
	return c.Message != nil && c.Message.Date == 0
}

func (c CallbackQuery) GetInaccessibleMessage() InaccessibleMessage {
	if c.Message == nil {
		return InaccessibleMessage{}
	}
	return InaccessibleMessage{
		Chat:      c.Message.Chat,
		MessageID: c.Message.MessageID,
	}
}

// ForceReply when receiving a message with this object, Telegram clients will
// display a reply interface to the user (act as if the user has selected the
// bot's message and tapped 'Reply'). This can be extremely useful if you  want
// to create user-friendly step-by-step interfaces without having to sacrifice
// privacy mode.
type ForceReply struct {
	// ForceReply shows reply interface to the user,
	// as if they manually selected the bot's message and tapped 'Reply'.
	ForceReply bool `json:"force_reply"`
	// InputFieldPlaceholder is the placeholder to be shown in the input field when
	// the reply is active; 1-64 characters.
	//
	// optional
	InputFieldPlaceholder string `json:"input_field_placeholder,omitempty"`
	// Selective use this parameter if you want to force reply from specific users only.
	// Targets:
	//  1) users that are @mentioned in the text of the Message object;
	//  2) if the bot's message is a reply (has Message.ReplyToMessage not nil), sender of the original message.
	//
	// optional
	Selective bool `json:"selective,omitempty"`
}

// ChatPhoto represents a chat photo.
type ChatPhoto struct {
	// SmallFileID is a file identifier of small (160x160) chat photo.
	// This file_id can be used only for photo download and
	// only for as long as the photo is not changed.
	SmallFileID string `json:"small_file_id"`
	// SmallFileUniqueID is a unique file identifier of small (160x160) chat
	// photo, which is supposed to be the same over time and for different bots.
	// Can't be used to download or reuse the file.
	SmallFileUniqueID string `json:"small_file_unique_id"`
	// BigFileID is a file identifier of big (640x640) chat photo.
	// This file_id can be used only for photo download and
	// only for as long as the photo is not changed.
	BigFileID string `json:"big_file_id"`
	// BigFileUniqueID is a file identifier of big (640x640) chat photo, which
	// is supposed to be the same over time and for different bots. Can't be
	// used to download or reuse the file.
	BigFileUniqueID string `json:"big_file_unique_id"`
}

// ChatInviteLink represents an invite link for a chat.
type ChatInviteLink struct {
	// InviteLink is the invite link. If the link was created by another chat
	// administrator, then the second part of the link will be replaced with “…”.
	InviteLink string `json:"invite_link"`
	// Creator of the link.
	Creator User `json:"creator"`
	// CreatesJoinRequest is true if users joining the chat via the link need to
	// be approved by chat administrators.
	//
	// optional
	CreatesJoinRequest bool `json:"creates_join_request,omitempty"`
	// IsPrimary is true, if the link is primary.
	IsPrimary bool `json:"is_primary"`
	// IsRevoked is true, if the link is revoked.
	IsRevoked bool `json:"is_revoked"`
	// Name is the name of the invite link.
	//
	// optional
	Name string `json:"name,omitempty"`
	// ExpireDate is the point in time (Unix timestamp) when the link will
	// expire or has been expired.
	//
	// optional
	ExpireDate int `json:"expire_date,omitempty"`
	// MemberLimit is the maximum number of users that can be members of the
	// chat simultaneously after joining the chat via this invite link; 1-99999.
	//
	// optional
	MemberLimit int `json:"member_limit,omitempty"`
	// PendingJoinRequestCount is the number of pending join requests created
	// using this link.
	//
	// optional
	PendingJoinRequestCount int `json:"pending_join_request_count,omitempty"`
	// SubscriptionPeriod is the number of seconds the subscription
	// will be active for before the next payment
	//
	// optional
	SubscriptionPeriod int `json:"subscription_period,omitempty"`
	// SubscriptionPrice is the amount of Telegram Stars a user
	// must pay initially and after each subsequent subscription
	// period to be a member of the chat using the link
	//
	// optional
	SubscriptionPrice int `json:"subscription_price,omitempty"`
}

type ChatAdministratorRights struct {
	IsAnonymous         bool `json:"is_anonymous"`
	CanManageChat       bool `json:"can_manage_chat"`
	CanDeleteMessages   bool `json:"can_delete_messages"`
	CanManageVideoChats bool `json:"can_manage_video_chats"`
	CanRestrictMembers  bool `json:"can_restrict_members"`
	CanPromoteMembers   bool `json:"can_promote_members"`
	CanChangeInfo       bool `json:"can_change_info"`
	CanInviteUsers      bool `json:"can_invite_users"`
	CanPostMessages     bool `json:"can_post_messages"`
	CanEditMessages     bool `json:"can_edit_messages"`
	CanPinMessages      bool `json:"can_pin_messages"`
	CanPostStories      bool `json:"can_post_stories"`
	CanEditStories      bool `json:"can_edit_stories"`
	CanDeleteStories    bool `json:"can_delete_stories"`
	CanManageTopics     bool `json:"can_manage_topics"`
}

// ChatMember contains information about one member of a chat.
type ChatMember struct {
	// User information about the user
	User *User `json:"user"`
	// Status the member's status in the chat.
	// Can be
	//  “creator”,
	//  “administrator”,
	//  “member”,
	//  “restricted”,
	//  “left” or
	//  “kicked”
	Status string `json:"status"`
	// CustomTitle owner and administrators only. Custom title for this user
	//
	// optional
	CustomTitle string `json:"custom_title,omitempty"`
	// IsAnonymous owner and administrators only. True, if the user's presence
	// in the chat is hidden
	//
	// optional
	IsAnonymous bool `json:"is_anonymous,omitempty"`
	// UntilDate for restricted and kicked.
	// Date when restrictions will be lifted for this user;
	// unix time.
	//
	// Until date for member.
	// Date when the user's subscription will expire;
	// Unix time
	//
	// optional
	UntilDate int64 `json:"until_date,omitempty"`
	// CanBeEdited administrators only.
	// True, if the bot is allowed to edit administrator privileges of that user.
	//
	// optional
	CanBeEdited bool `json:"can_be_edited,omitempty"`
	// CanManageChat administrators only.
	// True, if the administrator can access the chat event log, chat
	// statistics, message statistics in channels, see channel members, see
	// anonymous administrators in supergroups and ignore slow mode. Implied by
	// any other administrator privilege.
	//
	// optional
	CanManageChat bool `json:"can_manage_chat,omitempty"`
	// CanPostMessages administrators only.
	// True, if the administrator can post in the channel;
	// channels only.
	//
	// optional
	CanPostMessages bool `json:"can_post_messages,omitempty"`
	// CanEditMessages administrators only.
	// True, if the administrator can edit messages of other users and can pin messages;
	// channels only.
	//
	// optional
	CanEditMessages bool `json:"can_edit_messages,omitempty"`
	// CanDeleteMessages administrators only.
	// True, if the administrator can delete messages of other users.
	//
	// optional
	CanDeleteMessages bool `json:"can_delete_messages,omitempty"`
	// CanManageVideoChats administrators only.
	// True, if the administrator can manage video chats.
	//
	// optional
	CanManageVideoChats bool `json:"can_manage_video_chats,omitempty"`
	// CanRestrictMembers administrators only.
	// True, if the administrator can restrict, ban or unban chat members.
	//
	// optional
	CanRestrictMembers bool `json:"can_restrict_members,omitempty"`
	// CanPromoteMembers administrators only.
	// True, if the administrator can add new administrators
	// with a subset of their own privileges or demote administrators that he has promoted,
	// directly or indirectly (promoted by administrators that were appointed by the user).
	//
	// optional
	CanPromoteMembers bool `json:"can_promote_members,omitempty"`
	// CanChangeInfo administrators and restricted only.
	// True, if the user is allowed to change the chat title, photo and other settings.
	//
	// optional
	CanChangeInfo bool `json:"can_change_info,omitempty"`
	// CanInviteUsers administrators and restricted only.
	// True, if the user is allowed to invite new users to the chat.
	//
	// optional
	CanInviteUsers bool `json:"can_invite_users,omitempty"`
	// CanPinMessages administrators and restricted only.
	// True, if the user is allowed to pin messages; groups and supergroups only
	//
	// optional
	CanPinMessages bool `json:"can_pin_messages,omitempty"`
	// CanPostStories administrators only.
	// True, if the administrator can post stories in the channel; channels only
	//
	// optional
	CanPostStories bool `json:"can_post_stories,omitempty"`
	// CanEditStories administrators only.
	// True, if the administrator can edit stories posted by other users; channels only
	//
	// optional
	CanEditStories bool `json:"can_edit_stories,omitempty"`
	// CanDeleteStories administrators only.
	// True, if the administrator can delete stories posted by other users; channels only
	//
	// optional
	CanDeleteStories bool `json:"can_delete_stories,omitempty"`
	// CanManageTopics administrators and restricted only.
	// True, if the user is allowed to create, rename,
	// close, and reopen forum topics; supergroups only
	//
	// optional
	CanManageTopics bool `json:"can_manage_topics,omitempty"`
	// IsMember is true, if the user is a member of the chat at the moment of
	// the request
	IsMember bool `json:"is_member"`
	// CanSendMessages
	//
	// optional
	CanSendMessages bool `json:"can_send_messages,omitempty"`
	// CanSendAudios restricted only.
	// True, if the user is allowed to send audios
	//
	// optional
	CanSendAudios bool `json:"can_send_audios,omitempty"`
	// CanSendDocuments restricted only.
	// True, if the user is allowed to send documents
	//
	// optional
	CanSendDocuments bool `json:"can_send_documents,omitempty"`
	// CanSendPhotos is restricted only.
	// True, if the user is allowed to send photos
	//
	// optional
	CanSendPhotos bool `json:"can_send_photos,omitempty"`
	// CanSendVideos restricted only.
	// True, if the user is allowed to send videos
	//
	// optional
	CanSendVideos bool `json:"can_send_videos,omitempty"`
	// CanSendVideoNotes restricted only.
	// True, if the user is allowed to send video notes
	//
	// optional
	CanSendVideoNotes bool `json:"can_send_video_notes,omitempty"`
	// CanSendVoiceNotes restricted only.
	// True, if the user is allowed to send voice notes
	//
	// optional
	CanSendVoiceNotes bool `json:"can_send_voice_notes,omitempty"`
	// CanSendPolls restricted only.
	// True, if the user is allowed to send polls
	//
	// optional
	CanSendPolls bool `json:"can_send_polls,omitempty"`
	// CanSendOtherMessages restricted only.
	// True, if the user is allowed to send audios, documents,
	// photos, videos, video notes and voice notes.
	//
	// optional
	CanSendOtherMessages bool `json:"can_send_other_messages,omitempty"`
	// CanAddWebPagePreviews restricted only.
	// True, if the user is allowed to add web page previews to their messages.
	//
	// optional
	CanAddWebPagePreviews bool `json:"can_add_web_page_previews,omitempty"`
}

// IsCreator returns if the ChatMember was the creator of the chat.
func (chat ChatMember) IsCreator() bool { return chat.Status == "creator" }

// IsAdministrator returns if the ChatMember is a chat administrator.
func (chat ChatMember) IsAdministrator() bool { return chat.Status == "administrator" }

// HasLeft returns if the ChatMember left the chat.
func (chat ChatMember) HasLeft() bool { return chat.Status == "left" }

// WasKicked returns if the ChatMember was kicked from the chat.
func (chat ChatMember) WasKicked() bool { return chat.Status == "kicked" }

// SetCanSendMediaMessages is a method to replace field "can_send_media_messages".
// It sets CanSendAudio, CanSendDocuments, CanSendPhotos, CanSendVideos,
// CanSendVideoNotes, CanSendVoiceNotes to passed value.
func (chat *ChatMember) SetCanSendMediaMessages(b bool) {
	chat.CanSendAudios = b
	chat.CanSendDocuments = b
	chat.CanSendPhotos = b
	chat.CanSendVideos = b
	chat.CanSendVideoNotes = b
	chat.CanSendVoiceNotes = b
}

// CanSendMediaMessages method to replace field "can_send_media_messages".
// It returns true if CanSendAudio and CanSendDocuments and CanSendPhotos and CanSendVideos and
// CanSendVideoNotes and CanSendVoiceNotes are true.
func (chat *ChatMember) CanSendMediaMessages() bool {
	return chat.CanSendAudios && chat.CanSendDocuments &&
		chat.CanSendPhotos && chat.CanSendVideos &&
		chat.CanSendVideoNotes && chat.CanSendVoiceNotes
}

// ChatMemberUpdated represents changes in the status of a chat member.
type ChatMemberUpdated struct {
	// Chat the user belongs to.
	Chat Chat `json:"chat"`
	// From is the performer of the action, which resulted in the change.
	From User `json:"from"`
	// Date the change was done in Unix time.
	Date int `json:"date"`
	// Previous information about the chat member.
	OldChatMember ChatMember `json:"old_chat_member"`
	// New information about the chat member.
	NewChatMember ChatMember `json:"new_chat_member"`
	// InviteLink is the link which was used by the user to join the chat;
	// for joining by invite link events only.
	//
	// optional
	InviteLink *ChatInviteLink `json:"invite_link,omitempty"`
	// ViaJoinRequest is true, if the user joined the chat
	// after sending a direct join request
	// and being approved by an administrator
	//
	// optional
	ViaJoinRequest bool `json:"via_join_request,omitempty"`
	// ViaChatFolderInviteLink is True, if the user joined the chat
	// via a chat folder invite link
	//
	// optional
	ViaChatFolderInviteLink bool `json:"via_chat_folder_invite_link,omitempty"`
}

// ChatJoinRequest represents a join request sent to a chat.
type ChatJoinRequest struct {
	// Chat to which the request was sent.
	Chat Chat `json:"chat"`
	// User that sent the join request.
	From User `json:"from"`
	// UserChatID identifier of a private chat with the user who sent the join request.
	UserChatID int64 `json:"user_chat_id"`
	// Date the request was sent in Unix time.
	Date int `json:"date"`
	// Bio of the user.
	//
	// optional
	Bio string `json:"bio,omitempty"`
	// InviteLink is the link that was used by the user to send the join request.
	//
	// optional
	InviteLink *ChatInviteLink `json:"invite_link,omitempty"`
}

// ChatPermissions describes actions that a non-administrator user is
// allowed to take in a chat. All fields are optional.
type ChatPermissions struct {
	// CanSendMessages is true, if the user is allowed to send text messages,
	// contacts, locations and venues
	//
	// optional
	CanSendMessages bool `json:"can_send_messages,omitempty"`
	// CanSendAudios is true, if the user is allowed to send audios
	//
	// optional
	CanSendAudios bool `json:"can_send_audios,omitempty"`
	// CanSendDocuments is true, if the user is allowed to send documents
	//
	// optional
	CanSendDocuments bool `json:"can_send_documents,omitempty"`
	// CanSendPhotos is true, if the user is allowed to send photos
	//
	// optional
	CanSendPhotos bool `json:"can_send_photos,omitempty"`
	// CanSendVideos is true, if the user is allowed to send videos
	//
	// optional
	CanSendVideos bool `json:"can_send_videos,omitempty"`
	// CanSendVideoNotes is true, if the user is allowed to send video notes
	//
	// optional
	CanSendVideoNotes bool `json:"can_send_video_notes,omitempty"`
	// CanSendVoiceNotes is true, if the user is allowed to send voice notes
	//
	// optional
	CanSendVoiceNotes bool `json:"can_send_voice_notes,omitempty"`
	// CanSendPolls is true, if the user is allowed to send polls, implies
	// can_send_messages
	//
	// optional
	CanSendPolls bool `json:"can_send_polls,omitempty"`
	// CanSendOtherMessages is true, if the user is allowed to send animations,
	// games, stickers and use inline bots, implies can_send_media_messages
	//
	// optional
	CanSendOtherMessages bool `json:"can_send_other_messages,omitempty"`
	// CanAddWebPagePreviews is true, if the user is allowed to add web page
	// previews to their messages, implies can_send_media_messages
	//
	// optional
	CanAddWebPagePreviews bool `json:"can_add_web_page_previews,omitempty"`
	// CanChangeInfo is true, if the user is allowed to change the chat title,
	// photo and other settings. Ignored in public supergroups
	//
	// optional
	CanChangeInfo bool `json:"can_change_info,omitempty"`
	// CanInviteUsers is true, if the user is allowed to invite new users to the
	// chat
	//
	// optional
	CanInviteUsers bool `json:"can_invite_users,omitempty"`
	// CanPinMessages is true, if the user is allowed to pin messages. Ignored
	// in public supergroups
	//
	// optional
	CanPinMessages bool `json:"can_pin_messages,omitempty"`
	// CanManageTopics is true, if the user is allowed to create forum topics.
	// If omitted defaults to the value of can_pin_messages
	//
	// optional
	CanManageTopics bool `json:"can_manage_topics,omitempty"`
}

// SetCanSendMediaMessages is a method to replace field "can_send_media_messages".
// It sets CanSendAudio, CanSendDocuments, CanSendPhotos, CanSendVideos,
// CanSendVideoNotes, CanSendVoiceNotes to passed value.
func (c *ChatPermissions) SetCanSendMediaMessages(b bool) {
	c.CanSendAudios = b
	c.CanSendDocuments = b
	c.CanSendPhotos = b
	c.CanSendVideos = b
	c.CanSendVideoNotes = b
	c.CanSendVoiceNotes = b
}

// CanSendMediaMessages method to replace field "can_send_media_messages".
// It returns true if CanSendAudio and CanSendDocuments and CanSendPhotos and CanSendVideos and
// CanSendVideoNotes and CanSendVoiceNotes are true.
func (c *ChatPermissions) CanSendMediaMessages() bool {
	return c.CanSendAudios && c.CanSendDocuments &&
		c.CanSendPhotos && c.CanSendVideos &&
		c.CanSendVideoNotes && c.CanSendVoiceNotes
}

// Birthdate represents a user's birthdate
type Birthdate struct {
	// Day of the user's birth; 1-31
	Day int `json:"day"`
	// Month of the user's birth; 1-12
	Month int `json:"month"`
	// Year of the user's birth
	//
	// optional
	Year *int `json:"year,omitempty"`
}

// BusinessIntro represents a basic information about your business
type BusinessIntro struct {
	// Title text of the business intro
	//
	// optional
	Title *string `json:"title,omitempty"`
	// Message text of the business intro
	//
	// optional
	Message *string `json:"message,omitempty"`
	// Sticker of the business intro
	//
	// optional
	Sticker *Sticker `json:"sticker,omitempty"`
}

// BusinessLocation represents a business geodata
type BusinessLocation struct {
	// Address of the business
	Address string `json:"address"`
	// Location of the business
	//
	// optional
	Location *Location `json:"location,omitempty"`
}

// BusinessOpeningHoursInterval represents a business working interval
type BusinessOpeningHoursInterval struct {
	// OpeningMinute is the minute's sequence number in a week, starting on Monday,
	// marking the start of the time interval during which the business is open; 0 - 7 * 24 * 60
	OpeningMinute int `json:"opening_minute"`
	// ClosingMinute is the minute's sequence number in a week, starting on Monday,
	// marking the end of the time interval during which the business is open; 0 - 8 * 24 * 60
	ClosingMinute int `json:"closing_minute"`
}

// BusinessOpeningHours represents a set of business working intervals
type BusinessOpeningHours struct {
	// TimeZoneName is the unique name of the time zone
	// for which the opening hours are defined
	TimeZoneName string `json:"time_zone_name"`
	// OpeningHours is the list of time intervals describing
	// business opening hours
	OpeningHours []BusinessOpeningHoursInterval `json:"opening_hours"`
}

// ChatLocation represents a location to which a chat is connected.
type ChatLocation struct {
	// Location is the location to which the supergroup is connected. Can't be a
	// live location.
	Location Location `json:"location"`
	// Address is the location address; 1-64 characters, as defined by the chat
	// owner
	Address string `json:"address"`
}

const (
	ReactionTypeEmoji       = "emoji"
	ReactionTypeCustomEmoji = "custom_emoji"
	ReactionTypePaid        = "paid"
)

// ReactionType describes the type of a reaction.
// Currently, it can be one of: "emoji", "custom_emoji", "paid"
type ReactionType struct {
	// Type of the reaction. Can be "emoji", "custom_emoji", "paid"
	Type string `json:"type"`
	// Emoji type "emoji" only. Is a reaction emoji.
	Emoji string `json:"emoji"`
	// CustomEmoji type "custom_emoji" only. Is a custom emoji identifier.
	CustomEmoji string `json:"custom_emoji"`
}

func (r ReactionType) IsEmoji() bool {
	return r.Type == ReactionTypeEmoji
}

func (r ReactionType) IsCustomEmoji() bool {
	return r.Type == ReactionTypeCustomEmoji
}

func (r ReactionType) IsPaid() bool {
	return r.Type == ReactionTypePaid
}

// ReactionCount represents a reaction added to a message along with the number of times it was added.
type ReactionCount struct {
	// Type of the reaction
	Type ReactionType `json:"type"`
	// TotalCount number of times the reaction was added
	TotalCount int `json:"total_count"`
}

// MessageReactionUpdated represents a change of a reaction on a message performed by a user.
type MessageReactionUpdated struct {
	// Chat containing the message the user reacted to.
	Chat Chat `json:"chat"`
	// MessageID unique identifier of the message inside the chat.
	MessageID int `json:"message_id"`
	// User that changed the reaction, if the user isn't anonymous.
	//
	// optional
	User *User `json:"user"`
	// ActorChat the chat on behalf of which the reaction was changed,
	// if the user is anonymous.
	//
	// optional
	ActorChat *Chat `json:"actor_chat"`
	// Date of the change in Unix time.
	Date int64 `json:"date"`
	// OldReaction is a previous list of reaction types that were set by the user.
	OldReaction []ReactionType `json:"old_reaction"`
	// NewReaction is a new list of reaction types that have been set by the user.
	NewReaction []ReactionType `json:"new_reaction"`
}

// MessageReactionCountUpdated represents reaction changes on a message with anonymous reactions.
type MessageReactionCountUpdated struct {
	// Chat containing the message.
	Chat Chat `json:"chat"`
	// MessageID unique identifier of the message inside the chat.
	MessageID int `json:"message_id"`
	// Date of the change in Unix time.
	Date int64 `json:"date"`
	// Reactions is a list of reactions that are present on the message.
	Reactions []ReactionCount `json:"reactions"`
}

// ForumTopic represents a forum topic.
type ForumTopic struct {
	// MessageThreadID is the unique identifier of the forum topic
	MessageThreadID int `json:"message_thread_id"`
	// Name is the name of the topic
	Name string `json:"name"`
	// IconColor is the color of the topic icon in RGB format
	IconColor int `json:"icon_color"`
	// IconCustomEmojiID is the unique identifier of the custom emoji
	// shown as the topic icon
	//
	// optional
	IconCustomEmojiID string `json:"icon_custom_emoji_id,omitempty"`
}

// BotCommand represents a bot command.
type BotCommand struct {
	// Command text of the command, 1-32 characters.
	// Can contain only lowercase English letters, digits and underscores.
	Command string `json:"command"`
	// Description of the command, 3-256 characters.
	Description string `json:"description"`
}

// BotCommandScope represents the scope to which bot commands are applied.
//
// It contains the fields for all types of scopes, different types only support
// specific (or no) fields.
type BotCommandScope struct {
	Type   string `json:"type"`
	ChatID int64  `json:"chat_id,omitempty"`
	UserID int64  `json:"user_id,omitempty"`
}

// BotName represents the bot's name.
type BotName struct {
	Name string `json:"name"`
}

// BotDescription represents the bot's description.
type BotDescription struct {
	Description string `json:"description"`
}

// BotShortDescription represents the bot's short description
type BotShortDescription struct {
	ShortDescription string `json:"short_description"`
}

// MenuButton describes the bot's menu button in a private chat.
type MenuButton struct {
	// Type is the type of menu button, must be one of:
	// - `commands`
	// - `web_app`
	// - `default`
	Type string `json:"type"`
	// Text is the text on the button, for `web_app` type.
	Text string `json:"text,omitempty"`
	// WebApp is the description of the Web App that will be launched when the
	// user presses the button for the `web_app` type.
	//
	// Alternatively, a t.me link to a Web App of the bot can be specified in the object instead of the Web App's URL,
	// in which case the Web App will be opened as if the user pressed the link.
	WebApp *WebAppInfo `json:"web_app,omitempty"`
}

const (
	ChatBoostSourcePremium  = "premium"
	ChatBoostSourceGiftCode = "gift_code"
	ChatBoostSourceGiveaway = "giveaway"
)

// ChatBoostSource describes the source of a chat boost
type ChatBoostSource struct {
	// Source of the boost, It can be one of:
	// "premium", "gift_code", "giveaway"
	Source string `json:"source"`
	// For "premium": User that boosted the chat
	// For "gift_code": User for which the gift code was created
	// Optional for "giveaway": User that won the prize in the giveaway if any
	User *User `json:"user,omitempty"`
	// GiveawayMessageID "giveaway" only.
	// Is an identifier of a message in the chat with the giveaway;
	// the message could have been deleted already. May be 0 if the message isn't sent yet.
	GiveawayMessageID int `json:"giveaway_message_id,omitempty"`
	// PrizeStarCount "giveaway" only.
	// The number of Telegram Stars to be split
	// between giveaway winners;
	// for Telegram Star giveaways only
	//
	// optional
	PrizeStarCount int `json:"prize_star_count,omitempty"`
	// IsUnclaimed "giveaway" only.
	// True, if the giveaway was completed, but there was no user to win the prize
	//
	// optional
	IsUnclaimed bool `json:"is_unclaimed,omitempty"`
}

func (c ChatBoostSource) IsPremium() bool {
	return c.Source == ChatBoostSourcePremium
}

func (c ChatBoostSource) IsGiftCode() bool {
	return c.Source == ChatBoostSourceGiftCode
}

func (c ChatBoostSource) IsGiveaway() bool {
	return c.Source == ChatBoostSourceGiveaway
}

// ChatBoost contains information about a chat boost.
type ChatBoost struct {
	// BoostID is an unique identifier of the boost
	BoostID string `json:"boost_id"`
	// AddDate is a point in time (Unix timestamp) when the chat was boosted
	AddDate int64 `json:"add_date"`
	// ExpirationDate is a point in time (Unix timestamp) when the boost will
	// automatically expire, unless the booster's Telegram Premium subscription is prolonged
	ExpirationDate int64 `json:"expiration_date"`
	// Source of the added boost
	Source ChatBoostSource `json:"source"`
}

// ChatBoostUpdated represents a boost added to a chat or changed.
type ChatBoostUpdated struct {
	// Chat which was boosted
	Chat Chat `json:"chat"`
	// Boost infomation about the chat boost
	Boost ChatBoost `json:"boost"`
}

// ChatBoostRemoved represents a boost removed from a chat.
type ChatBoostRemoved struct {
	// Chat which was boosted
	Chat Chat `json:"chat"`
	// BoostID unique identifier of the boost
	BoostID string `json:"boost_id"`
	// RemoveDate point in time (Unix timestamp) when the boost was removed
	RemoveDate int64 `json:"remove_date"`
	// Source of the removed boost
	Source ChatBoostSource `json:"source"`
}

// UserChatBoosts represents a list of boosts added to a chat by a user.
type UserChatBoosts struct {
	// Boosts is the list of boosts added to the chat by the user
	Boosts []ChatBoost `json:"boosts"`
}

// BusinessConnection describes the connection of the bot with a business account.
type BusinessConnection struct {
	// ID is an unique identifier of the business connection
	ID string `json:"id"`
	// User is a business account user that created the business connection
	User User `json:"user"`
	// UserChatID identifier of a private chat with the user who
	// created the business connection.
	UserChatID int64 `json:"user_chat_id"`
	// Date the connection was established in Unix time
	Date int64 `json:"date"`
	// CanReply is True, if the bot can act on behalf of the
	// business account in chats that were active in the last 24 hours
	CanReply bool `json:"can_reply"`
	// IsEnabled is True, if the connection is active
	IsEnabled bool `json:"is_enabled"`
}

// BusinessMessagesDeleted is received when messages are deleted
// from a connected business account.
type BusinessMessagesDeleted struct {
	// BusinessConnectionID is an unique identifier
	// of the business connection
	BusinessConnectionID string `json:"business_connection_id"`
	// Chat is the information about a chat in the business account.
	// The bot may not have access to the chat or the corresponding user.
	Chat Chat `json:"chat"`
	// MessageIDs is a JSON-serialized list of identifiers of deleted messages
	// in the chat of the business account
	MessageIDs []int `json:"message_ids"`
}

// ResponseParameters are various errors that can be returned in APIResponse.
type ResponseParameters struct {
	// The group has been migrated to a supergroup with the specified identifier.
	//
	// optional
	MigrateToChatID int64 `json:"migrate_to_chat_id,omitempty"`
	// In case of exceeding flood control, the number of seconds left to wait
	// before the request can be repeated.
	//
	// optional
	RetryAfter int `json:"retry_after,omitempty"`
}

// BaseInputMedia is a base type for the InputMedia types.
type BaseInputMedia struct {
	// Type of the result.
	Type string `json:"type"`
	// Media file to send. Pass a file_id to send a file
	// that exists on the Telegram servers (recommended),
	// pass an HTTP URL for Telegram to get a file from the Internet,
	// or pass “attach://<file_attach_name>” to upload a new one
	// using multipart/form-data under <file_attach_name> name.
	Media RequestFileData `json:"media"`
	// thumb intentionally missing as it is not currently compatible

	// Caption of the video to be sent, 0-1024 characters after entities parsing.
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// ParseMode mode for parsing entities in the video caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// Pass True, if the caption must be shown above the message media
	//
	// optional
	ShowCaptionAboveMedia bool `json:"show_caption_above_media,omitempty"`
	// HasSpoiler pass True, if the photo needs to be covered with a spoiler animation
	//
	// optional
	HasSpoiler bool `json:"has_spoiler,omitempty"`
}

// InputMediaPhoto is a photo to send as part of a media group.
type InputMediaPhoto struct {
	BaseInputMedia
}

// InputMediaVideo is a video to send as part of a media group.
type InputMediaVideo struct {
	BaseInputMedia
	// Thumbnail of the file sent; can be ignored if thumbnail generation for
	// the file is supported server-side.
	//
	// optional
	Thumb RequestFileData `json:"thumbnail,omitempty"`
	// Width video width
	//
	// optional
	Width int `json:"width,omitempty"`
	// Height video height
	//
	// optional
	Height int `json:"height,omitempty"`
	// Duration video duration
	//
	// optional
	Duration int `json:"duration,omitempty"`
	// SupportsStreaming pass True, if the uploaded video is suitable for streaming.
	//
	// optional
	SupportsStreaming bool `json:"supports_streaming,omitempty"`
	// HasSpoiler pass True, if the video needs to be covered with a spoiler animation
	//
	// optional
	HasSpoiler bool `json:"has_spoiler,omitempty"`
}

// InputMediaAnimation is an animation to send as part of a media group.
type InputMediaAnimation struct {
	BaseInputMedia
	// Thumbnail of the file sent; can be ignored if thumbnail generation for
	// the file is supported server-side.
	//
	// optional
	Thumb RequestFileData `json:"thumbnail,omitempty"`
	// Width video width
	//
	// optional
	Width int `json:"width,omitempty"`
	// Height video height
	//
	// optional
	Height int `json:"height,omitempty"`
	// Duration video duration
	//
	// optional
	Duration int `json:"duration,omitempty"`
	// HasSpoiler pass True, if the photo needs to be covered with a spoiler animation
	//
	// optional
	HasSpoiler bool `json:"has_spoiler,omitempty"`
}

// InputMediaAudio is an audio to send as part of a media group.
type InputMediaAudio struct {
	BaseInputMedia
	// Thumbnail of the file sent; can be ignored if thumbnail generation for
	// the file is supported server-side.
	//
	// optional
	Thumb RequestFileData `json:"thumbnail,omitempty"`
	// Duration of the audio in seconds
	//
	// optional
	Duration int `json:"duration,omitempty"`
	// Performer of the audio
	//
	// optional
	Performer string `json:"performer,omitempty"`
	// Title of the audio
	//
	// optional
	Title string `json:"title,omitempty"`
}

// InputMediaDocument is a general file to send as part of a media group.
type InputMediaDocument struct {
	BaseInputMedia
	// Thumbnail of the file sent; can be ignored if thumbnail generation for
	// the file is supported server-side.
	//
	// optional
	Thumb RequestFileData `json:"thumbnail,omitempty"`
	// DisableContentTypeDetection disables automatic server-side content type
	// detection for files uploaded using multipart/form-data. Always true, if
	// the document is sent as part of an album
	//
	// optional
	DisableContentTypeDetection bool `json:"disable_content_type_detection,omitempty"`
}

// This object describes the paid media to be sent. Currently, it can be one of:
//   - InputPaidMediaPhoto
//   - InputPaidMediaVideo
type InputPaidMedia struct {
	// Type of the media, must be one of:
	//  - "photo"
	//  - "video"
	Type string `json:"type"`
	// File to send. Pass a file_id to send a file that exists on the Telegram servers (recommended),
	// pass an HTTP URL for Telegram to get a file from the Internet,
	// or pass “attach://<file_attach_name>” to upload a new one using multipart/form-data under <file_attach_name> name.
	// More information on https://core.telegram.org/bots/api#sending-files
	Media RequestFileData `json:"media"`
	// InputPaidMediaVideo only.
	// Thumbnail of the file sent; can be ignored if thumbnail generation for the file is supported server-side.
	// The thumbnail should be in JPEG format and less than 200 kB in size.
	// A thumbnail's width and height should not exceed 320. Ignored if the file is not uploaded using multipart/form-data.
	// Thumbnails can't be reused and can be only uploaded as a new file,
	//  so you can pass “attach://<file_attach_name>” if the thumbnail was uploaded using multipart/form-data under <file_attach_name>.
	//
	// optional
	Thumb RequestFileData `json:"thumbnail"`
	// InputPaidMediaVideo only.
	// Video width
	//
	// optional
	Width int64 `json:"width,omitempty"`
	// InputPaidMediaVideo only.
	// Video height
	//
	// optional
	Height int64 `json:"height,omitempty"`
	// InputPaidMediaVideo only.
	// Video duration in seconds
	//
	// optional
	Duration int64 `json:"duration,omitempty"`
	// InputPaidMediaVideo only.
	// Pass True if the uploaded video is suitable for streaming
	SupportsStreaming bool `json:"supports_streaming,omitempty"`
}

// Constant values for sticker types
const (
	StickerTypeRegular     = "regular"
	StickerTypeMask        = "mask"
	StickerTypeCustomEmoji = "custom_emoji"
)

// Sticker represents a sticker.
type Sticker struct {
	// FileID is an identifier for this file, which can be used to download or
	// reuse the file
	FileID string `json:"file_id"`
	// FileUniqueID is a unique identifier for this file,
	// which is supposed to be the same over time and for different bots.
	// Can't be used to download or reuse the file.
	FileUniqueID string `json:"file_unique_id"`
	// Type is a type of the sticker, currently one of “regular”,
	// “mask”, “custom_emoji”. The type of the sticker is independent
	// from its format, which is determined by the fields is_animated and is_video.
	Type string `json:"type"`
	// Width sticker width
	Width int `json:"width"`
	// Height sticker height
	Height int `json:"height"`
	// IsAnimated true, if the sticker is animated
	//
	// optional
	IsAnimated bool `json:"is_animated,omitempty"`
	// IsVideo true, if the sticker is a video sticker
	//
	// optional
	IsVideo bool `json:"is_video,omitempty"`
	// Thumbnail sticker thumbnail in the .WEBP or .JPG format
	//
	// optional
	Thumbnail *PhotoSize `json:"thumbnail,omitempty"`
	// Emoji associated with the sticker
	//
	// optional
	Emoji string `json:"emoji,omitempty"`
	// SetName of the sticker set to which the sticker belongs
	//
	// optional
	SetName string `json:"set_name,omitempty"`
	// PremiumAnimation for premium regular stickers, premium animation for the sticker
	//
	// optional
	PremiumAnimation *File `json:"premium_animation,omitempty"`
	// MaskPosition is for mask stickers, the position where the mask should be
	// placed
	//
	// optional
	MaskPosition *MaskPosition `json:"mask_position,omitempty"`
	// CustomEmojiID for custom emoji stickers, unique identifier of the custom emoji
	//
	// optional
	CustomEmojiID string `json:"custom_emoji_id,omitempty"`
	// NeedsRepainting True, if the sticker must be repainted to a text color in messages, the color of the Telegram Premium badge in emoji status, white color on chat photos, or another appropriate color in other places
	//
	//optional
	NeedsRepainting bool `json:"needs_repainting,omitempty"`
	// FileSize
	//
	// optional
	FileSize int `json:"file_size,omitempty"`
}

// IsRegular returns if the Sticker is regular
func (s Sticker) IsRegular() bool {
	return s.Type == StickerTypeRegular
}

// IsMask returns if the Sticker is mask
func (s Sticker) IsMask() bool {
	return s.Type == StickerTypeMask
}

// IsCustomEmoji returns if the Sticker is custom emoji
func (s Sticker) IsCustomEmoji() bool {
	return s.Type == StickerTypeCustomEmoji
}

// StickerSet represents a sticker set.
type StickerSet struct {
	// Name sticker set name
	Name string `json:"name"`
	// Title sticker set title
	Title string `json:"title"`
	// StickerType of stickers in the set, currently one of “regular”, “mask”, “custom_emoji”
	StickerType string `json:"sticker_type"`
	// ContainsMasks true, if the sticker set contains masks
	//
	// deprecated. Use sticker_type instead
	ContainsMasks bool `json:"contains_masks"`
	// Stickers list of all set stickers
	Stickers []Sticker `json:"stickers"`
	// Thumb is the sticker set thumbnail in the .WEBP or .TGS format
	Thumbnail *PhotoSize `json:"thumbnail"`
}

// IsRegular returns if the StickerSet is regular
func (s StickerSet) IsRegular() bool {
	return s.StickerType == StickerTypeRegular
}

// IsMask returns if the StickerSet is mask
func (s StickerSet) IsMask() bool {
	return s.StickerType == StickerTypeMask
}

// IsCustomEmoji returns if the StickerSet is custom emoji
func (s StickerSet) IsCustomEmoji() bool {
	return s.StickerType == StickerTypeCustomEmoji
}

// MaskPosition describes the position on faces where a mask should be placed
// by default.
type MaskPosition struct {
	// The part of the face relative to which the mask should be placed.
	// One of “forehead”, “eyes”, “mouth”, or “chin”.
	Point string `json:"point"`
	// Shift by X-axis measured in widths of the mask scaled to the face size,
	// from left to right. For example, choosing -1.0 will place mask just to
	// the left of the default mask position.
	XShift float64 `json:"x_shift"`
	// Shift by Y-axis measured in heights of the mask scaled to the face size,
	// from top to bottom. For example, 1.0 will place the mask just below the
	// default mask position.
	YShift float64 `json:"y_shift"`
	// Mask scaling coefficient. For example, 2.0 means double size.
	Scale float64 `json:"scale"`
}

// InputSticker describes a sticker to be added to a sticker set.
type InputSticker struct {
	// The added sticker. Pass a file_id as a String to send a file that already exists on the Telegram servers, pass an HTTP URL as a String for Telegram to get a file from the Internet, upload a new one using multipart/form-data, or pass “attach://<file_attach_name>” to upload a new one using multipart/form-data under <file_attach_name> name. Animated and video stickers can't be uploaded via HTTP URL.
	Sticker RequestFile `json:"sticker"`
	// 	Format of the added sticker, must be one of “static” for a
	// .WEBP or .PNG image, “animated” for a .TGS animation, “video” for a WEBM video
	Format string `json:"format"`
	// List of 1-20 emoji associated with the sticker
	EmojiList []string `json:"emoji_list"`
	// Position where the mask should be placed on faces. For “mask” stickers only.
	//
	// optional
	MaskPosition *MaskPosition `json:"mask_position"`
	// List of 0-20 search keywords for the sticker with total length of up to 64 characters. For “regular” and “custom_emoji” stickers only.
	//
	// optional
	Keywords []string `json:"keywords"`
}

// Game represents a game. Use BotFather to create and edit games, their short
// names will act as unique identifiers.
type Game struct {
	// Title of the game
	Title string `json:"title"`
	// Description of the game
	Description string `json:"description"`
	// Photo that will be displayed in the game message in chats.
	Photo []PhotoSize `json:"photo"`
	// Text a brief description of the game or high scores included in the game message.
	// Can be automatically edited to include current high scores for the game
	// when the bot calls setGameScore, or manually edited using editMessageText. 0-4096 characters.
	//
	// optional
	Text string `json:"text,omitempty"`
	// TextEntities special entities that appear in text, such as usernames, URLs, bot commands, etc.
	//
	// optional
	TextEntities []MessageEntity `json:"text_entities,omitempty"`
	// Animation is an animation that will be displayed in the game message in chats.
	// Upload via BotFather (https://t.me/botfather).
	//
	// optional
	Animation Animation `json:"animation,omitempty"`
}

// GameHighScore is a user's score and position on the leaderboard.
type GameHighScore struct {
	// Position in high score table for the game
	Position int `json:"position"`
	// User user
	User User `json:"user"`
	// Score score
	Score int `json:"score"`
}

// CallbackGame is for starting a game in an inline keyboard button.
type CallbackGame struct{}

// SwitchInlineQueryChosenChat represents an inline button that switches the current
// user to inline mode in a chosen chat, with an optional default inline query.
type SwitchInlineQueryChosenChat struct {
	// Query is default inline query to be inserted in the input field.
	// If left empty, only the bot's username will be inserted
	//
	// optional
	Query string `json:"query,omitempty"`
	// AllowUserChats is True, if private chats with users can be chosen
	//
	// optional
	AllowUserChats bool `json:"allow_user_chats,omitempty"`
	// AllowBotChats is True, if private chats with bots can be chosen
	//
	// optional
	AllowBotChats bool `json:"allow_bot_chats,omitempty"`
	// AllowGroupChats is True, if group and supergroup chats can be chosen
	//
	// optional
	AllowGroupChats bool `json:"allow_group_chats,omitempty"`
	// AllowChannelChats is True, if channel chats can be chosen
	//
	// optional
	AllowChannelChats bool `json:"allow_channel_chats,omitempty"`
}

// WebhookInfo is information about a currently set webhook.
type WebhookInfo struct {
	// URL webhook URL, may be empty if webhook is not set up.
	URL string `json:"url"`
	// HasCustomCertificate true, if a custom certificate was provided for webhook certificate checks.
	HasCustomCertificate bool `json:"has_custom_certificate"`
	// PendingUpdateCount number of updates awaiting delivery.
	PendingUpdateCount int `json:"pending_update_count"`
	// IPAddress is the currently used webhook IP address
	//
	// optional
	IPAddress string `json:"ip_address,omitempty"`
	// LastErrorDate unix time for the most recent error
	// that happened when trying to deliver an update via webhook.
	//
	// optional
	LastErrorDate int `json:"last_error_date,omitempty"`
	// LastErrorMessage error message in human-readable format for the most recent error
	// that happened when trying to deliver an update via webhook.
	//
	// optional
	LastErrorMessage string `json:"last_error_message,omitempty"`
	// LastSynchronizationErrorDate is the unix time of the most recent error that
	// happened when trying to synchronize available updates with Telegram datacenters.
	LastSynchronizationErrorDate int `json:"last_synchronization_error_date,omitempty"`
	// MaxConnections maximum allowed number of simultaneous
	// HTTPS connections to the webhook for update delivery.
	//
	// optional
	MaxConnections int `json:"max_connections,omitempty"`
	// AllowedUpdates is a list of update types the bot is subscribed to.
	// Defaults to all update types
	//
	// optional
	AllowedUpdates []string `json:"allowed_updates,omitempty"`
}

// IsSet returns true if a webhook is currently set.
func (info WebhookInfo) IsSet() bool {
	return info.URL != ""
}

// InlineQuery is a Query from Telegram for an inline request.
type InlineQuery struct {
	// ID unique identifier for this query
	ID string `json:"id"`
	// From sender
	From *User `json:"from"`
	// Query text of the query (up to 256 characters).
	Query string `json:"query"`
	// Offset of the results to be returned, can be controlled by the bot.
	Offset string `json:"offset"`
	// Type of the chat, from which the inline query was sent. Can be either
	// “sender” for a private chat with the inline query sender, “private”,
	// “group”, “supergroup”, or “channel”. The chat type should be always known
	// for requests sent from official clients and most third-party clients,
	// unless the request was sent from a secret chat
	//
	// optional
	ChatType string `json:"chat_type,omitempty"`
	// Location sender location, only for bots that request user location.
	//
	// optional
	Location *Location `json:"location,omitempty"`
}

// InlineQueryResultCachedAudio is an inline query response with cached audio.
type InlineQueryResultCachedAudio struct {
	// Type of the result, must be audio
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes
	ID string `json:"id"`
	// AudioID a valid file identifier for the audio file
	AudioID string `json:"audio_file_id"`
	// Caption 0-1024 characters after entities parsing
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// ParseMode mode for parsing entities in the video caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the audio
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultCachedDocument is an inline query response with cached document.
type InlineQueryResultCachedDocument struct {
	// Type of the result, must be a document
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes
	ID string `json:"id"`
	// DocumentID a valid file identifier for the file
	DocumentID string `json:"document_file_id"`
	// Title for the result
	//
	// optional
	Title string `json:"title,omitempty"`
	// Caption of the document to be sent, 0-1024 characters after entities parsing
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// Description short description of the result
	//
	// optional
	Description string `json:"description,omitempty"`
	// ParseMode mode for parsing entities in the video caption.
	//	// See formatting options for more details
	//	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the file
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultCachedGIF is an inline query response with cached gif.
type InlineQueryResultCachedGIF struct {
	// Type of the result, must be gif.
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes.
	ID string `json:"id"`
	// GifID a valid file identifier for the GIF file.
	GIFID string `json:"gif_file_id"`
	// Title for the result
	//
	// optional
	Title string `json:"title,omitempty"`
	// Caption of the GIF file to be sent, 0-1024 characters after entities parsing.
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// ParseMode mode for parsing entities in the caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// Pass True, if the caption must be shown above the message media
	//
	// optional
	ShowCaptionAboveMedia bool `json:"show_caption_above_media,omitempty"`
	// ReplyMarkup inline keyboard attached to the message.
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the GIF animation.
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultCachedMPEG4GIF is an inline query response with cached
// H.264/MPEG-4 AVC video without sound gif.
type InlineQueryResultCachedMPEG4GIF struct {
	// Type of the result, must be mpeg4_gif
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes
	ID string `json:"id"`
	// MPEG4FileID a valid file identifier for the MP4 file
	MPEG4FileID string `json:"mpeg4_file_id"`
	// Title for the result
	//
	// optional
	Title string `json:"title,omitempty"`
	// Caption of the MPEG-4 file to be sent, 0-1024 characters after entities parsing.
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// ParseMode mode for parsing entities in the caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// ParseMode mode for parsing entities in the video caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// Pass True, if the caption must be shown above the message media
	//
	// optional
	ShowCaptionAboveMedia bool `json:"show_caption_above_media,omitempty"`
	// ReplyMarkup inline keyboard attached to the message.
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the video animation.
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultCachedPhoto is an inline query response with cached photo.
type InlineQueryResultCachedPhoto struct {
	// Type of the result, must be a photo.
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes.
	ID string `json:"id"`
	// PhotoID a valid file identifier of the photo.
	PhotoID string `json:"photo_file_id"`
	// Title for the result.
	//
	// optional
	Title string `json:"title,omitempty"`
	// Description short description of the result.
	//
	// optional
	Description string `json:"description,omitempty"`
	// Caption of the photo to be sent, 0-1024 characters after entities parsing.
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// ParseMode mode for parsing entities in the photo caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// Pass True, if the caption must be shown above the message media
	//
	// optional
	ShowCaptionAboveMedia bool `json:"show_caption_above_media,omitempty"`
	// ReplyMarkup inline keyboard attached to the message.
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the photo.
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultCachedSticker is an inline query response with cached sticker.
type InlineQueryResultCachedSticker struct {
	// Type of the result, must be a sticker
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes
	ID string `json:"id"`
	// StickerID a valid file identifier of the sticker
	StickerID string `json:"sticker_file_id"`
	// Title is a title
	Title string `json:"title"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the sticker
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultCachedVideo is an inline query response with cached video.
type InlineQueryResultCachedVideo struct {
	// Type of the result, must be video
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes
	ID string `json:"id"`
	// VideoID a valid file identifier for the video file
	VideoID string `json:"video_file_id"`
	// Title for the result
	Title string `json:"title"`
	// Description short description of the result
	//
	// optional
	Description string `json:"description,omitempty"`
	// Caption of the video to be sent, 0-1024 characters after entities parsing
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// ParseMode mode for parsing entities in the video caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// Pass True, if the caption must be shown above the message media
	//
	// optional
	ShowCaptionAboveMedia bool `json:"show_caption_above_media,omitempty"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the video
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultCachedVoice is an inline query response with cached voice.
type InlineQueryResultCachedVoice struct {
	// Type of the result, must be voice
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes
	ID string `json:"id"`
	// VoiceID a valid file identifier for the voice message
	VoiceID string `json:"voice_file_id"`
	// Title voice message title
	Title string `json:"title"`
	// Caption 0-1024 characters after entities parsing
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// ParseMode mode for parsing entities in the video caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the voice message
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultArticle represents a link to an article or web page.
type InlineQueryResultArticle struct {
	// Type of the result, must be article.
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 Bytes.
	ID string `json:"id"`
	// Title of the result
	Title string `json:"title"`
	// InputMessageContent content of the message to be sent.
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
	// ReplyMarkup Inline keyboard attached to the message.
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// URL of the result.
	//
	// optional
	URL string `json:"url,omitempty"`
	// HideURL pass True, if you don't want the URL to be shown in the message.
	//
	// optional
	HideURL bool `json:"hide_url,omitempty"`
	// Description short description of the result.
	//
	// optional
	Description string `json:"description,omitempty"`
	// ThumbURL url of the thumbnail for the result
	//
	// optional
	ThumbURL string `json:"thumbnail_url,omitempty"`
	// ThumbWidth thumbnail width
	//
	// optional
	ThumbWidth int `json:"thumbnail_width,omitempty"`
	// ThumbHeight thumbnail height
	//
	// optional
	ThumbHeight int `json:"thumbnail_height,omitempty"`
}

// InlineQueryResultAudio is an inline query response audio.
type InlineQueryResultAudio struct {
	// Type of the result, must be audio
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes
	ID string `json:"id"`
	// URL a valid url for the audio file
	URL string `json:"audio_url"`
	// Title is a title
	Title string `json:"title"`
	// Caption 0-1024 characters after entities parsing
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// ParseMode mode for parsing entities in the video caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// Performer is a performer
	//
	// optional
	Performer string `json:"performer,omitempty"`
	// Duration audio duration in seconds
	//
	// optional
	Duration int `json:"audio_duration,omitempty"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the audio
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultContact is an inline query response contact.
type InlineQueryResultContact struct {
	Type                string                `json:"type"`         // required
	ID                  string                `json:"id"`           // required
	PhoneNumber         string                `json:"phone_number"` // required
	FirstName           string                `json:"first_name"`   // required
	LastName            string                `json:"last_name"`
	VCard               string                `json:"vcard"`
	ReplyMarkup         *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	InputMessageContent interface{}           `json:"input_message_content,omitempty"`
	ThumbURL            string                `json:"thumbnail_url"`
	ThumbWidth          int                   `json:"thumbnail_width"`
	ThumbHeight         int                   `json:"thumbnail_height"`
}

// InlineQueryResultGame is an inline query response game.
type InlineQueryResultGame struct {
	// Type of the result, must be game
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes
	ID string `json:"id"`
	// GameShortName short name of the game
	GameShortName string `json:"game_short_name"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

// InlineQueryResultDocument is an inline query response document.
type InlineQueryResultDocument struct {
	// Type of the result, must be a document
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes
	ID string `json:"id"`
	// Title for the result
	Title string `json:"title"`
	// Caption of the document to be sent, 0-1024 characters after entities parsing
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// URL a valid url for the file
	URL string `json:"document_url"`
	// MimeType of the content of the file, either “application/pdf” or “application/zip”
	MimeType string `json:"mime_type"`
	// Description short description of the result
	//
	// optional
	Description string `json:"description,omitempty"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the file
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
	// ThumbURL url of the thumbnail (jpeg only) for the file
	//
	// optional
	ThumbURL string `json:"thumbnail_url,omitempty"`
	// ThumbWidth thumbnail width
	//
	// optional
	ThumbWidth int `json:"thumbnail_width,omitempty"`
	// ThumbHeight thumbnail height
	//
	// optional
	ThumbHeight int `json:"thumbnail_height,omitempty"`
}

// InlineQueryResultGIF is an inline query response GIF.
type InlineQueryResultGIF struct {
	// Type of the result, must be gif.
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes.
	ID string `json:"id"`
	// URL a valid URL for the GIF file. File size must not exceed 1MB.
	URL string `json:"gif_url"`
	// ThumbURL url of the static (JPEG or GIF) or animated (MPEG4) thumbnail for the result.
	ThumbURL string `json:"thumbnail_url"`
	// MIME type of the thumbnail, must be one of “image/jpeg”, “image/gif”, or “video/mp4”. Defaults to “image/jpeg”
	ThumbMimeType string `json:"thumbnail_mime_type,omitempty"`
	// Width of the GIF
	//
	// optional
	Width int `json:"gif_width,omitempty"`
	// Height of the GIF
	//
	// optional
	Height int `json:"gif_height,omitempty"`
	// Duration of the GIF
	//
	// optional
	Duration int `json:"gif_duration,omitempty"`
	// Title for the result
	//
	// optional
	Title string `json:"title,omitempty"`
	// Caption of the GIF file to be sent, 0-1024 characters after entities parsing.
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// ParseMode mode for parsing entities in the video caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// Pass True, if the caption must be shown above the message media
	//
	// optional
	ShowCaptionAboveMedia bool `json:"show_caption_above_media,omitempty"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the GIF animation.
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultLocation is an inline query response location.
type InlineQueryResultLocation struct {
	// Type of the result, must be location
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 Bytes
	ID string `json:"id"`
	// Latitude  of the location in degrees
	Latitude float64 `json:"latitude"`
	// Longitude of the location in degrees
	Longitude float64 `json:"longitude"`
	// Title of the location
	Title string `json:"title"`
	// HorizontalAccuracy is the radius of uncertainty for the location,
	// measured in meters; 0-1500
	//
	// optional
	HorizontalAccuracy float64 `json:"horizontal_accuracy,omitempty"`
	// LivePeriod is the period in seconds for which the location can be
	// updated, should be between 60 and 86400.
	//
	// optional
	LivePeriod int `json:"live_period,omitempty"`
	// Heading is for live locations, a direction in which the user is moving,
	// in degrees. Must be between 1 and 360 if specified.
	//
	// optional
	Heading int `json:"heading,omitempty"`
	// ProximityAlertRadius is for live locations, a maximum distance for
	// proximity alerts about approaching another chat member, in meters. Must
	// be between 1 and 100000 if specified.
	//
	// optional
	ProximityAlertRadius int `json:"proximity_alert_radius,omitempty"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the location
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
	// ThumbURL url of the thumbnail for the result
	//
	// optional
	ThumbURL string `json:"thumbnail_url,omitempty"`
	// ThumbWidth thumbnail width
	//
	// optional
	ThumbWidth int `json:"thumbnail_width,omitempty"`
	// ThumbHeight thumbnail height
	//
	// optional
	ThumbHeight int `json:"thumbnail_height,omitempty"`
}

// InlineQueryResultMPEG4GIF is an inline query response MPEG4 GIF.
type InlineQueryResultMPEG4GIF struct {
	// Type of the result, must be mpeg4_gif
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes
	ID string `json:"id"`
	// URL a valid URL for the MP4 file. File size must not exceed 1MB
	URL string `json:"mpeg4_url"`
	// Width video width
	//
	// optional
	Width int `json:"mpeg4_width,omitempty"`
	// Height vVideo height
	//
	// optional
	Height int `json:"mpeg4_height,omitempty"`
	// Duration video duration
	//
	// optional
	Duration int `json:"mpeg4_duration,omitempty"`
	// ThumbURL url of the static (JPEG or GIF) or animated (MPEG4) thumbnail for the result.
	ThumbURL string `json:"thumbnail_url"`
	// MIME type of the thumbnail, must be one of “image/jpeg”, “image/gif”, or “video/mp4”. Defaults to “image/jpeg”
	ThumbMimeType string `json:"thumbnail_mime_type,omitempty"`
	// Title for the result
	//
	// optional
	Title string `json:"title,omitempty"`
	// Caption of the MPEG-4 file to be sent, 0-1024 characters after entities parsing.
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// ParseMode mode for parsing entities in the video caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// Pass True, if the caption must be shown above the message media
	//
	// optional
	ShowCaptionAboveMedia bool `json:"show_caption_above_media,omitempty"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the video animation
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultPhoto is an inline query response photo.
type InlineQueryResultPhoto struct {
	// Type of the result, must be article.
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 Bytes.
	ID string `json:"id"`
	// URL a valid URL of the photo. Photo must be in jpeg format.
	// Photo size must not exceed 5MB.
	URL string `json:"photo_url"`
	// MimeType
	MimeType string `json:"mime_type"`
	// Width of the photo
	//
	// optional
	Width int `json:"photo_width,omitempty"`
	// Height of the photo
	//
	// optional
	Height int `json:"photo_height,omitempty"`
	// ThumbURL url of the thumbnail for the photo.
	//
	// optional
	ThumbURL string `json:"thumbnail_url,omitempty"`
	// Title for the result
	//
	// optional
	Title string `json:"title,omitempty"`
	// Description short description of the result
	//
	// optional
	Description string `json:"description,omitempty"`
	// Caption of the photo to be sent, 0-1024 characters after entities parsing.
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// ParseMode mode for parsing entities in the photo caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// ReplyMarkup inline keyboard attached to the message.
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// Pass True, if the caption must be shown above the message media
	//
	// optional
	ShowCaptionAboveMedia bool `json:"show_caption_above_media,omitempty"`
	// InputMessageContent content of the message to be sent instead of the photo.
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultVenue is an inline query response venue.
type InlineQueryResultVenue struct {
	// Type of the result, must be venue
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 Bytes
	ID string `json:"id"`
	// Latitude of the venue location in degrees
	Latitude float64 `json:"latitude"`
	// Longitude of the venue location in degrees
	Longitude float64 `json:"longitude"`
	// Title of the venue
	Title string `json:"title"`
	// Address of the venue
	Address string `json:"address"`
	// FoursquareID foursquare identifier of the venue if known
	//
	// optional
	FoursquareID string `json:"foursquare_id,omitempty"`
	// FoursquareType foursquare type of the venue, if known.
	// (For example, “arts_entertainment/default”, “arts_entertainment/aquarium” or “food/icecream”.)
	//
	// optional
	FoursquareType string `json:"foursquare_type,omitempty"`
	// GooglePlaceID is the Google Places identifier of the venue
	//
	// optional
	GooglePlaceID string `json:"google_place_id,omitempty"`
	// GooglePlaceType is the Google Places type of the venue
	//
	// optional
	GooglePlaceType string `json:"google_place_type,omitempty"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the venue
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
	// ThumbURL url of the thumbnail for the result
	//
	// optional
	ThumbURL string `json:"thumbnail_url,omitempty"`
	// ThumbWidth thumbnail width
	//
	// optional
	ThumbWidth int `json:"thumbnail_width,omitempty"`
	// ThumbHeight thumbnail height
	//
	// optional
	ThumbHeight int `json:"thumbnail_height,omitempty"`
}

// InlineQueryResultVideo is an inline query response video.
type InlineQueryResultVideo struct {
	// Type of the result, must be video
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes
	ID string `json:"id"`
	// URL a valid url for the embedded video player or video file
	URL string `json:"video_url"`
	// MimeType of the content of video url, “text/html” or “video/mp4”
	MimeType string `json:"mime_type"`
	//
	// ThumbURL url of the thumbnail (jpeg only) for the video
	// optional
	ThumbURL string `json:"thumbnail_url,omitempty"`
	// Title for the result
	Title string `json:"title"`
	// Caption of the video to be sent, 0-1024 characters after entities parsing
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// Pass True, if the caption must be shown above the message media
	//
	// optional
	ShowCaptionAboveMedia bool `json:"show_caption_above_media,omitempty"`
	// ParseMode mode for parsing entities in the video caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// Width video width
	//
	// optional
	Width int `json:"video_width,omitempty"`
	// Height video height
	//
	// optional
	Height int `json:"video_height,omitempty"`
	// Duration video duration in seconds
	//
	// optional
	Duration int `json:"video_duration,omitempty"`
	// Description short description of the result
	//
	// optional
	Description string `json:"description,omitempty"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the video.
	// This field is required if InlineQueryResultVideo is used to send
	// an HTML-page as a result (e.g., a YouTube video).
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// InlineQueryResultVoice is an inline query response voice.
type InlineQueryResultVoice struct {
	// Type of the result, must be voice
	Type string `json:"type"`
	// ID unique identifier for this result, 1-64 bytes
	ID string `json:"id"`
	// URL a valid URL for the voice recording
	URL string `json:"voice_url"`
	// Title recording title
	Title string `json:"title"`
	// Caption 0-1024 characters after entities parsing
	//
	// optional
	Caption string `json:"caption,omitempty"`
	// ParseMode mode for parsing entities in the voice caption.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// CaptionEntities is a list of special entities that appear in the caption,
	// which can be specified instead of parse_mode
	//
	// optional
	CaptionEntities []MessageEntity `json:"caption_entities,omitempty"`
	// Duration recording duration in seconds
	//
	// optional
	Duration int `json:"voice_duration,omitempty"`
	// ReplyMarkup inline keyboard attached to the message
	//
	// optional
	ReplyMarkup *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	// InputMessageContent content of the message to be sent instead of the voice recording
	//
	// optional
	InputMessageContent interface{} `json:"input_message_content,omitempty"`
}

// ChosenInlineResult is an inline query result chosen by a User
type ChosenInlineResult struct {
	// ResultID the unique identifier for the result that was chosen
	ResultID string `json:"result_id"`
	// From the user that chose the result
	From *User `json:"from"`
	// Location sender location, only for bots that require user location
	//
	// optional
	Location *Location `json:"location,omitempty"`
	// InlineMessageID identifier of the sent inline message.
	// Available only if there is an inline keyboard attached to the message.
	// Will be also received in callback queries and can be used to edit the message.
	//
	// optional
	InlineMessageID string `json:"inline_message_id,omitempty"`
	// Query the query that was used to obtain the result
	Query string `json:"query"`
}

// SentWebAppMessage contains information about an inline message sent by a Web App
// on behalf of a user.
type SentWebAppMessage struct {
	// Identifier of the sent inline message. Available only if there is an inline
	// keyboard attached to the message.
	//
	// optional
	InlineMessageID string `json:"inline_message_id,omitempty"`
}

// InputTextMessageContent contains text for displaying
// as an inline query result.
type InputTextMessageContent struct {
	// Text of the message to be sent, 1-4096 characters
	Text string `json:"message_text"`
	// ParseMode mode for parsing entities in the message text.
	// See formatting options for more details
	// (https://core.telegram.org/bots/api#formatting-options).
	//
	// optional
	ParseMode string `json:"parse_mode,omitempty"`
	// Entities is a list of special entities that appear in message text, which
	// can be specified instead of parse_mode
	//
	// optional
	Entities []MessageEntity `json:"entities,omitempty"`
	// LinkPreviewOptions used for link preview generation for the original message
	//
	// Optional
	LinkPreviewOptions *LinkPreviewOptions `json:"link_preview_options,omitempty"`
}

// InputLocationMessageContent contains a location for displaying
// as an inline query result.
type InputLocationMessageContent struct {
	// Latitude of the location in degrees
	Latitude float64 `json:"latitude"`
	// Longitude of the location in degrees
	Longitude float64 `json:"longitude"`
	// HorizontalAccuracy is the radius of uncertainty for the location,
	// measured in meters; 0-1500
	//
	// optional
	HorizontalAccuracy float64 `json:"horizontal_accuracy,omitempty"`
	// LivePeriod is the period in seconds for which the location can be
	// updated, should be between 60 and 86400
	//
	// optional
	LivePeriod int `json:"live_period,omitempty"`
	// Heading is for live locations, a direction in which the user is moving,
	// in degrees. Must be between 1 and 360 if specified.
	//
	// optional
	Heading int `json:"heading,omitempty"`
	// ProximityAlertRadius is for live locations, a maximum distance for
	// proximity alerts about approaching another chat member, in meters. Must
	// be between 1 and 100000 if specified.
	//
	// optional
	ProximityAlertRadius int `json:"proximity_alert_radius,omitempty"`
}

// InputVenueMessageContent contains a venue for displaying
// as an inline query result.
type InputVenueMessageContent struct {
	// Latitude of the venue in degrees
	Latitude float64 `json:"latitude"`
	// Longitude of the venue in degrees
	Longitude float64 `json:"longitude"`
	// Title name of the venue
	Title string `json:"title"`
	// Address of the venue
	Address string `json:"address"`
	// FoursquareID foursquare identifier of the venue, if known
	//
	// optional
	FoursquareID string `json:"foursquare_id,omitempty"`
	// FoursquareType Foursquare type of the venue, if known
	//
	// optional
	FoursquareType string `json:"foursquare_type,omitempty"`
	// GooglePlaceID is the Google Places identifier of the venue
	//
	// optional
	GooglePlaceID string `json:"google_place_id,omitempty"`
	// GooglePlaceType is the Google Places type of the venue
	//
	// optional
	GooglePlaceType string `json:"google_place_type,omitempty"`
}

// InputContactMessageContent contains a contact for displaying
// as an inline query result.
type InputContactMessageContent struct {
	// 	PhoneNumber contact's phone number
	PhoneNumber string `json:"phone_number"`
	// FirstName contact's first name
	FirstName string `json:"first_name"`
	// LastName contact's last name
	//
	// optional
	LastName string `json:"last_name,omitempty"`
	// Additional data about the contact in the form of a vCard
	//
	// optional
	VCard string `json:"vcard,omitempty"`
}

// InputInvoiceMessageContent represents the content of an invoice message to be
// sent as the result of an inline query.
type InputInvoiceMessageContent struct {
	// Product name, 1-32 characters
	Title string `json:"title"`
	// Product description, 1-255 characters
	Description string `json:"description"`
	// Bot-defined invoice payload, 1-128 bytes. This will not be displayed to
	// the user, use for your internal processes.
	Payload string `json:"payload"`
	// Payment provider token, obtained via Botfather. Pass an empty string for payments in Telegram Stars.
	//
	// optional
	ProviderToken string `json:"provider_token"`
	// Three-letter ISO 4217 currency code. Pass “XTR” for payments in Telegram Stars.
	Currency string `json:"currency"`
	// Price breakdown, a JSON-serialized list of components (e.g. product
	// price, tax, discount, delivery cost, delivery tax, bonus, etc.)
	Prices []LabeledPrice `json:"prices"`
	// The maximum accepted amount for tips in the smallest units of the
	// currency (integer, not float/double).
	//
	// optional
	MaxTipAmount int `json:"max_tip_amount,omitempty"`
	// An array of suggested amounts of tip in the smallest units of the
	// currency (integer, not float/double). At most 4 suggested tip amounts can
	// be specified. The suggested tip amounts must be positive, passed in a
	// strictly increased order and must not exceed max_tip_amount.
	//
	// optional
	SuggestedTipAmounts []int `json:"suggested_tip_amounts,omitempty"`
	// A JSON-serialized object for data about the invoice, which will be shared
	// with the payment provider. A detailed description of the required fields
	// should be provided by the payment provider.
	//
	// optional
	ProviderData string `json:"provider_data,omitempty"`
	// URL of the product photo for the invoice. Can be a photo of the goods or
	// a marketing image for a service. People like it better when they see what
	// they are paying for.
	//
	// optional
	PhotoURL string `json:"photo_url,omitempty"`
	// Photo size
	//
	// optional
	PhotoSize int `json:"photo_size,omitempty"`
	// Photo width
	//
	// optional
	PhotoWidth int `json:"photo_width,omitempty"`
	// Photo height
	//
	// optional
	PhotoHeight int `json:"photo_height,omitempty"`
	// Pass True, if you require the user's full name to complete the order
	//
	// optional
	NeedName bool `json:"need_name,omitempty"`
	// Pass True, if you require the user's phone number to complete the order
	//
	// optional
	NeedPhoneNumber bool `json:"need_phone_number,omitempty"`
	// Pass True, if you require the user's email address to complete the order
	//
	// optional
	NeedEmail bool `json:"need_email,omitempty"`
	// Pass True, if you require the user's shipping address to complete the order
	//
	// optional
	NeedShippingAddress bool `json:"need_shipping_address,omitempty"`
	// Pass True, if user's phone number should be sent to provider
	//
	// optional
	SendPhoneNumberToProvider bool `json:"send_phone_number_to_provider,omitempty"`
	// Pass True, if user's email address should be sent to provider
	//
	// optional
	SendEmailToProvider bool `json:"send_email_to_provider,omitempty"`
	// Pass True, if the final price depends on the shipping method
	//
	// optional
	IsFlexible bool `json:"is_flexible,omitempty"`
}

// LabeledPrice represents a portion of the price for goods or services.
type LabeledPrice struct {
	// Label portion label
	Label string `json:"label"`
	// Amount price of the product in the smallest units of the currency (integer, not float/double).
	// For example, for a price of US$ 1.45 pass amount = 145.
	// See the exp parameter in currencies.json
	// (https://core.telegram.org/bots/payments/currencies.json),
	// it shows the number of digits past the decimal point
	// for each currency (2 for the majority of currencies).
	Amount int `json:"amount"`
}

// Invoice contains basic information about an invoice.
type Invoice struct {
	// Title product name
	Title string `json:"title"`
	// Description product description
	Description string `json:"description"`
	// StartParameter unique bot deep-linking parameter that can be used to generate this invoice
	StartParameter string `json:"start_parameter"`
	// Currency three-letter ISO 4217 currency code, or “XTR” for payments in Telegram Stars
	// (see https://core.telegram.org/bots/payments#supported-currencies)
	Currency string `json:"currency"`
	// TotalAmount total price in the smallest units of the currency (integer, not float/double).
	// For example, for a price of US$ 1.45 pass amount = 145.
	// See the exp parameter in currencies.json
	// (https://core.telegram.org/bots/payments/currencies.json),
	// it shows the number of digits past the decimal point
	// for each currency (2 for the majority of currencies).
	TotalAmount int `json:"total_amount"`
}

// ShippingAddress represents a shipping address.
type ShippingAddress struct {
	// CountryCode ISO 3166-1 alpha-2 country code
	CountryCode string `json:"country_code"`
	// State if applicable
	State string `json:"state"`
	// City city
	City string `json:"city"`
	// StreetLine1 first line for the address
	StreetLine1 string `json:"street_line1"`
	// StreetLine2 second line for the address
	StreetLine2 string `json:"street_line2"`
	// PostCode address post code
	PostCode string `json:"post_code"`
}

// OrderInfo represents information about an order.
type OrderInfo struct {
	// Name user name
	//
	// optional
	Name string `json:"name,omitempty"`
	// PhoneNumber user's phone number
	//
	// optional
	PhoneNumber string `json:"phone_number,omitempty"`
	// Email user email
	//
	// optional
	Email string `json:"email,omitempty"`
	// ShippingAddress user shipping address
	//
	// optional
	ShippingAddress *ShippingAddress `json:"shipping_address,omitempty"`
}

// ShippingOption represents one shipping option.
type ShippingOption struct {
	// ID shipping option identifier
	ID string `json:"id"`
	// Title option title
	Title string `json:"title"`
	// Prices list of price portions
	Prices []LabeledPrice `json:"prices"`
}

// SuccessfulPayment contains basic information about a successful payment.
type SuccessfulPayment struct {
	// Currency three-letter ISO 4217 currency code, or “XTR” for payments in Telegram Stars
	// (see https://core.telegram.org/bots/payments#supported-currencies)
	Currency string `json:"currency"`
	// TotalAmount total price in the smallest units of the currency (integer, not float/double).
	// For example, for a price of US$ 1.45 pass amount = 145.
	// See the exp parameter in currencies.json,
	// (https://core.telegram.org/bots/payments/currencies.json)
	// it shows the number of digits past the decimal point
	// for each currency (2 for the majority of currencies).
	TotalAmount int `json:"total_amount"`
	// InvoicePayload bot specified invoice payload
	InvoicePayload string `json:"invoice_payload"`
	// ShippingOptionID identifier of the shipping option chosen by the user
	//
	// optional
	ShippingOptionID string `json:"shipping_option_id,omitempty"`
	// OrderInfo order info provided by the user
	//
	// optional
	OrderInfo *OrderInfo `json:"order_info,omitempty"`
	// TelegramPaymentChargeID telegram payment identifier
	TelegramPaymentChargeID string `json:"telegram_payment_charge_id"`
	// ProviderPaymentChargeID provider payment identifier
	ProviderPaymentChargeID string `json:"provider_payment_charge_id"`
}

// RefundPayment contains basic information about a refunded payment.
type RefundedPayment struct {
	// Three-letter ISO 4217 currency code (https://core.telegram.org/bots/payments#supported-currencies),
	// or “XTR” for payments in Telegram Stars.
	// Currently, always “XTR”
	Currency string `json:"currency"`
	// Total refunded price in the smallest units of the currency (integer, not float/double).
	// For example, for a price of US$ 1.45, total_amount = 145.
	// See the exp parameter in https://core.telegram.org/bots/payments/currencies.json,
	// it shows the number of digits past the decimal point for each currency (2 for the majority of currencies).
	TotalAmount int64 `json:"total_amount"`
	// Bot-specified invoice payload
	InvoicePayload string `json:"invoice_payload"`
	// Telegram payment identifier
	TelegramPaymentChargeID string `json:"telegram_payment_charge_id"`
	// Provider payment identifier
	//
	// optional
	ProviderPaymentChargeID string `json:"provider_payment_charge_id,omitempty"`
}

// ShippingQuery contains information about an incoming shipping query.
type ShippingQuery struct {
	// ID unique query identifier
	ID string `json:"id"`
	// From user who sent the query
	From *User `json:"from"`
	// InvoicePayload bot specified invoice payload
	InvoicePayload string `json:"invoice_payload"`
	// ShippingAddress user specified shipping address
	ShippingAddress *ShippingAddress `json:"shipping_address"`
}

// PreCheckoutQuery contains information about an incoming pre-checkout query.
type PreCheckoutQuery struct {
	// ID unique query identifier
	ID string `json:"id"`
	// From user who sent the query
	From *User `json:"from"`
	// Currency three-letter ISO 4217 currency code, or “XTR” for payments in Telegram Stars
	//	// (see https://core.telegram.org/bots/payments#supported-currencies)
	Currency string `json:"currency"`
	// TotalAmount total price in the smallest units of the currency (integer, not float/double).
	//	// For example, for a price of US$ 1.45 pass amount = 145.
	//	// See the exp parameter in currencies.json,
	//	// (https://core.telegram.org/bots/payments/currencies.json)
	//	// it shows the number of digits past the decimal point
	//	// for each currency (2 for the majority of currencies).
	TotalAmount int `json:"total_amount"`
	// InvoicePayload bot specified invoice payload
	InvoicePayload string `json:"invoice_payload"`
	// ShippingOptionID identifier of the shipping option chosen by the user
	//
	// optional
	ShippingOptionID string `json:"shipping_option_id,omitempty"`
	// OrderInfo order info provided by the user
	//
	// optional
	OrderInfo *OrderInfo `json:"order_info,omitempty"`
}

// PaidMediaPurchased contains information about a paid media purchase.
type PaidMediaPurchased struct {
	// From is the user who purchased the media
	From User `json:"from"`
	// PaidMediaPayload bot-specified paid media payload
	PaidMediaPayload string `json:"paid_media_payload"`
}

// RevenueWithdrawalState describes the state of a revenue withdrawal operation.
// Currently, it can be one of
//   - RevenueWithdrawalStatePending
//   - RevenueWithdrawalStateSucceeded
//   - RevenueWithdrawalStateFailed
type RevenueWithdrawalState struct {
	// Type of the state. Must be one of:
	// 	- pending
	// 	- succeeded
	//  - failed
	Type string `json:"type"`
	// 	Date the withdrawal was completed in Unix time. Represents only in “succeeded” state
	Date int64 `json:"date,omitempty"`
	// An HTTPS URL that can be used to see transaction details.
	// Represents only in “succeeded” state
	URL string `json:"url,omitempty"`
}

// TransactionPartner describes the source of a transaction, or its recipient for outgoing transactions. Currently, it can be one of
//   - TransactionPartnerUser
//   - TransactionPartnerFragment
//   - TransactionPartnerTelegramAds
//   - TransactionPartnerOther
type TransactionPartner struct {
	//Type of the transaction partner. Must be one of:
	//	- fragment
	//	- user
	//  - other
	//  - telegram_ads
	Type string `json:"type"`
	// State of the transaction if the transaction is outgoing.
	// Represent only in "fragment" state
	//
	// optional
	WithdrawalState *RevenueWithdrawalState `json:"withdrawal_state,omitempty"`
	// Information about the user.
	// Represent only in "user" state
	User *User `json:"user,omitempty"`
	// TransactionPartnerUser only.
	// Bot-specified invoice payload
	//
	// optional
	InvoicePayload string `json:"invoice_payload,omitempty"`
	// PaidMedia is the nformation about the paid media
	// bought by the user
	// Represent only in "user" state
	//
	// optional
	PaidMedia []PaidMedia `json:"paid_media,omitempty"`
}

// StarTransaction describes a Telegram Star transaction.
type StarTransaction struct {
	// Unique identifier of the transaction.
	// Coincides with the identifer of the original transaction for refund transactions.
	// Coincides with SuccessfulPayment.telegram_payment_charge_id for successful incoming payments from users.
	ID string `json:"id"`
	// Number of Telegram Stars transferred by the transaction
	Amount int64 `json:"amount"`
	// Date the transaction was created in Unix time
	Date int64 `json:"date"`
	// Source of an incoming transaction (e.g., a user purchasing goods or services, Fragment refunding a failed withdrawal).
	// Only for incoming transactions
	//
	// optional
	Source *TransactionPartner `json:"source,omitempty"`
	// Receiver of an outgoing transaction (e.g., a user for a purchase refund, Fragment for a withdrawal).
	// Only for outgoing transactions
	//
	// optional
	Reciever *TransactionPartner `json:"reciever,omitempty"`
}

// StarTransactions contains a list of Telegram Star transactions.
type StarTransactions struct {
	// The list of transactions
	Transactions []StarTransaction `json:"transactions"`
}

package config

import (
	"time"
)

// Settings represents application configuration independent of source (CLI, DB, etc)
type Settings struct {
	// core settings
	InstanceID string `json:"instance_id" yaml:"instance_id" db:"instance_id"`

	// group settings by domain
	Telegram      TelegramSettings      `json:"telegram" yaml:"telegram" db:"telegram"`
	Admin         AdminSettings         `json:"admin" yaml:"admin" db:"admin"`
	History       HistorySettings       `json:"history" yaml:"history" db:"history"`
	Logger        LoggerSettings        `json:"logger" yaml:"logger" db:"logger"`
	CAS           CASSettings           `json:"cas" yaml:"cas" db:"cas"`
	Meta          MetaSettings          `json:"meta" yaml:"meta" db:"meta"`
	OpenAI        OpenAISettings        `json:"openai" yaml:"openai" db:"openai"`
	LuaPlugins    LuaPluginsSettings    `json:"lua_plugins" yaml:"lua_plugins" db:"lua_plugins"`
	AbnormalSpace AbnormalSpaceSettings `json:"abnormal_spacing" yaml:"abnormal_spacing" db:"abnormal_spacing"`
	Files         FilesSettings         `json:"files" yaml:"files" db:"files"`
	Message       MessageSettings       `json:"message" yaml:"message" db:"message"`
	Server        ServerSettings        `json:"server" yaml:"server" db:"server"`

	// spam detection settings
	SimilarityThreshold float64 `json:"similarity_threshold" yaml:"similarity_threshold" db:"similarity_threshold"`
	MinMsgLen           int     `json:"min_msg_len" yaml:"min_msg_len" db:"min_msg_len"`
	MaxEmoji            int     `json:"max_emoji" yaml:"max_emoji" db:"max_emoji"`
	MinSpamProbability  float64 `json:"min_spam_probability" yaml:"min_spam_probability" db:"min_spam_probability"`
	MultiLangWords      int     `json:"multi_lang_words" yaml:"multi_lang_words" db:"multi_lang_words"`

	// bot behavior settings
	NoSpamReply         bool `json:"no_spam_reply" yaml:"no_spam_reply" db:"no_spam_reply"`
	SuppressJoinMessage bool `json:"suppress_join_message" yaml:"suppress_join_message" db:"suppress_join_message"`

	// detection mode settings
	ParanoidMode       bool `json:"paranoid_mode" yaml:"paranoid_mode" db:"paranoid_mode"`
	FirstMessagesCount int  `json:"first_messages_count" yaml:"first_messages_count" db:"first_messages_count"`

	// operation mode settings
	Training   bool   `json:"training" yaml:"training" db:"training_mode"`
	SoftBan    bool   `json:"soft_ban" yaml:"soft_ban" db:"soft_ban_mode"`
	Convert    string `json:"convert" yaml:"convert" db:"convert_mode"`
	MaxBackups int    `json:"max_backups" yaml:"max_backups" db:"max_backups"`
	Dry        bool   `json:"dry" yaml:"dry" db:"dry_mode"`

	// transient fields that should never be stored
	Transient TransientSettings `json:"-" yaml:"-"`
}

// TelegramSettings contains Telegram-specific settings
type TelegramSettings struct {
	Group        string        `json:"group" yaml:"group" db:"telegram_group"`
	IdleDuration time.Duration `json:"idle_duration" yaml:"idle_duration" db:"telegram_idle_duration"`
	Timeout      time.Duration `json:"timeout" yaml:"timeout" db:"telegram_timeout"`
	Token        string        `json:"token" yaml:"token" db:"telegram_token"`
}

// AdminSettings contains admin-related settings
type AdminSettings struct {
	AdminGroup              string   `json:"admin_group" yaml:"admin_group" db:"admin_group"`
	DisableAdminSpamForward bool     `json:"disable_admin_spam_forward" yaml:"disable_admin_spam_forward" db:"disable_admin_spam_forward"`
	TestingIDs              []int64  `json:"testing_ids" yaml:"testing_ids" db:"testing_ids"`
	SuperUsers              []string `json:"super_users" yaml:"super_users" db:"super_users"`
}

// HistorySettings contains history-related settings
type HistorySettings struct {
	Duration time.Duration `json:"duration" yaml:"duration" db:"history_duration"`
	MinSize  int           `json:"min_size" yaml:"min_size" db:"history_min_size"`
	Size     int           `json:"size" yaml:"size" db:"history_size"`
}

// LoggerSettings contains logging-related settings
type LoggerSettings struct {
	Enabled    bool   `json:"enabled" yaml:"enabled" db:"logger_enabled"`
	FileName   string `json:"file_name" yaml:"file_name" db:"logger_file_name"`
	MaxSize    string `json:"max_size" yaml:"max_size" db:"logger_max_size"`
	MaxBackups int    `json:"max_backups" yaml:"max_backups" db:"logger_max_backups"`
}

// CASSettings contains Combot Anti-Spam System settings
type CASSettings struct {
	API       string        `json:"api" yaml:"api" db:"cas_api"`
	Timeout   time.Duration `json:"timeout" yaml:"timeout" db:"cas_timeout"`
	UserAgent string        `json:"user_agent" yaml:"user_agent" db:"cas_user_agent"`
}

// MetaSettings contains message metadata check settings
type MetaSettings struct {
	LinksLimit      int    `json:"links_limit" yaml:"links_limit" db:"meta_links_limit"`
	MentionsLimit   int    `json:"mentions_limit" yaml:"mentions_limit" db:"meta_mentions_limit"`
	ImageOnly       bool   `json:"image_only" yaml:"image_only" db:"meta_image_only"`
	LinksOnly       bool   `json:"links_only" yaml:"links_only" db:"meta_links_only"`
	VideosOnly      bool   `json:"videos_only" yaml:"videos_only" db:"meta_videos_only"`
	AudiosOnly      bool   `json:"audios_only" yaml:"audios_only" db:"meta_audios_only"`
	Forward         bool   `json:"forward" yaml:"forward" db:"meta_forward"`
	Keyboard        bool   `json:"keyboard" yaml:"keyboard" db:"meta_keyboard"`
	UsernameSymbols string `json:"username_symbols" yaml:"username_symbols" db:"meta_username_symbols"`
}

// OpenAISettings contains OpenAI integration settings
type OpenAISettings struct {
	APIBase            string   `json:"api_base" yaml:"api_base" db:"openai_api_base"`
	Veto               bool     `json:"veto" yaml:"veto" db:"openai_veto"`
	Prompt             string   `json:"prompt" yaml:"prompt" db:"openai_prompt"`
	CustomPrompts      []string `json:"custom_prompts" yaml:"custom_prompts" db:"openai_custom_prompts"`
	Model              string   `json:"model" yaml:"model" db:"openai_model"`
	Token              string   `json:"token" yaml:"token" db:"openai_token"`
	MaxTokensResponse  int      `json:"max_tokens_response" yaml:"max_tokens_response" db:"openai_max_tokens_response"`
	MaxTokensRequest   int      `json:"max_tokens_request" yaml:"max_tokens_request" db:"openai_max_tokens_request"`
	MaxSymbolsRequest  int      `json:"max_symbols_request" yaml:"max_symbols_request" db:"openai_max_symbols_request"`
	RetryCount         int      `json:"retry_count" yaml:"retry_count" db:"openai_retry_count"`
	HistorySize        int      `json:"history_size" yaml:"history_size" db:"openai_history_size"`
	ReasoningEffort    string   `json:"reasoning_effort" yaml:"reasoning_effort" db:"openai_reasoning_effort"`
	CheckShortMessages bool     `json:"check_short_messages" yaml:"check_short_messages" db:"openai_check_short_messages"`
}

// LuaPluginsSettings contains Lua plugins settings
type LuaPluginsSettings struct {
	Enabled        bool     `json:"enabled" yaml:"enabled" db:"lua_plugins_enabled"`
	PluginsDir     string   `json:"plugins_dir" yaml:"plugins_dir" db:"lua_plugins_dir"`
	EnabledPlugins []string `json:"enabled_plugins" yaml:"enabled_plugins" db:"lua_enabled_plugins"`
	DynamicReload  bool     `json:"dynamic_reload" yaml:"dynamic_reload" db:"lua_dynamic_reload"`
}

// AbnormalSpaceSettings contains abnormal spacing detection settings
type AbnormalSpaceSettings struct {
	Enabled                 bool    `json:"enabled" yaml:"enabled" db:"abnormal_spacing_enabled"`
	SpaceRatioThreshold     float64 `json:"space_ratio_threshold" yaml:"space_ratio_threshold" db:"abnormal_spacing_ratio"`
	ShortWordRatioThreshold float64 `json:"short_word_ratio_threshold" yaml:"short_word_ratio_threshold" db:"abnormal_spacing_short_ratio"`
	ShortWordLen            int     `json:"short_word_len" yaml:"short_word_len" db:"abnormal_spacing_short_word"`
	MinWords                int     `json:"min_words" yaml:"min_words" db:"abnormal_spacing_min_words"`
}

// FilesSettings contains file location settings
type FilesSettings struct {
	SamplesDataPath string `json:"samples_data_path" yaml:"samples_data_path" db:"files_samples_path"`
	DynamicDataPath string `json:"dynamic_data_path" yaml:"dynamic_data_path" db:"files_dynamic_path"`
	WatchInterval   int    `json:"watch_interval_secs" yaml:"watch_interval_secs" db:"files_watch_interval_secs"`
}

// MessageSettings contains message customization settings
type MessageSettings struct {
	Startup string `json:"startup" yaml:"startup" db:"message_startup"`
	Spam    string `json:"spam" yaml:"spam" db:"message_spam"`
	Dry     string `json:"dry" yaml:"dry" db:"message_dry"`
	Warn    string `json:"warn" yaml:"warn" db:"message_warn"`
}

// ServerSettings contains web server settings
type ServerSettings struct {
	Enabled    bool   `json:"enabled" yaml:"enabled" db:"server_enabled"`
	ListenAddr string `json:"listen_addr" yaml:"listen_addr" db:"server_listen_addr"`
	AuthUser   string `json:"auth_user" yaml:"auth_user" db:"server_auth_user"`
	AuthHash   string `json:"auth_hash" yaml:"auth_hash" db:"server_auth_hash"`
}

// TransientSettings contains settings that should never be persisted
type TransientSettings struct {
	// connection parameters
	DataBaseURL    string        `json:"-" yaml:"-"`
	StorageTimeout time.Duration `json:"-" yaml:"-"`

	// control flags
	ConfigDB bool `json:"-" yaml:"-"`
	Dbg      bool `json:"-" yaml:"-"`
	TGDbg    bool `json:"-" yaml:"-"`

	// encryption for database stored configuration
	ConfigDBEncryptKey string `json:"-" yaml:"-"`

	// temporary auth password (used only to generate hash)
	WebAuthPasswd string `json:"-" yaml:"-"`
}

// New creates a new settings instance
func New() *Settings {
	return &Settings{}
}

// IsOpenAIEnabled returns true if OpenAI integration is enabled
func (s *Settings) IsOpenAIEnabled() bool {
	return s.OpenAI.APIBase != "" || s.OpenAI.Token != ""
}

// IsMetaEnabled returns true if any meta check is enabled
func (s *Settings) IsMetaEnabled() bool {
	return s.Meta.ImageOnly ||
		s.Meta.LinksLimit >= 0 ||
		s.Meta.MentionsLimit >= 0 ||
		s.Meta.LinksOnly ||
		s.Meta.VideosOnly ||
		s.Meta.AudiosOnly ||
		s.Meta.Forward ||
		s.Meta.Keyboard ||
		s.Meta.UsernameSymbols != ""
}

// IsCASEnabled returns true if CAS integration is enabled
func (s *Settings) IsCASEnabled() bool {
	return s.CAS.API != ""
}

// IsStartupMessageEnabled returns true if a startup message is configured
func (s *Settings) IsStartupMessageEnabled() bool {
	return s.Message.Startup != ""
}

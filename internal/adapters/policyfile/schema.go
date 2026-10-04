package policyfile

import (
	"time"
)

// fileDoc is the YAML shape of spec section 6. Pointers distinguish "omitted"
// where the difference matters. Unknown keys are rejected by the decoder.
type fileDoc struct {
	Version      *int               `yaml:"version"`
	Profile      string             `yaml:"profile"`
	UPN          string             `yaml:"upn"`
	TenantID     string             `yaml:"tenant_id"`
	StateDir     string             `yaml:"state_dir"`
	Destinations map[string]destDoc `yaml:"destinations"`
	Instruct     instructDoc        `yaml:"instruct"`
	Inbound      inboundDoc         `yaml:"inbound"`
	Send         sendDoc            `yaml:"send"`
	Limits       limitsDoc          `yaml:"limits"`
	Audit        struct {
		Path string `yaml:"path"`
	} `yaml:"audit"`
	Selftest struct {
		NonMemberChatID string `yaml:"non_member_chat_id"`
	} `yaml:"selftest"`
}

type destDoc struct {
	TeamID      string `yaml:"team_id"`
	ChannelID   string `yaml:"channel_id"`
	ChatID      string `yaml:"chat_id"`
	AADID       string `yaml:"aad_id"`
	DisplayName string `yaml:"display_name"`
	Send        bool   `yaml:"send"`
	Watch       bool   `yaml:"watch"`
	CreateChat  bool   `yaml:"create_chat"`
}

type idsDoc struct {
	AADIDs []string `yaml:"aad_ids"`
}

type instructDoc struct {
	Commanders idsDoc `yaml:"commanders"`
	Agents     idsDoc `yaml:"agents"`
}

type inboundDoc struct {
	Handle        []string      `yaml:"handle"`
	MaxLookback   time.Duration `yaml:"max_lookback"`
	PollInterval  time.Duration `yaml:"poll_interval"`
	MaxWait       time.Duration `yaml:"max_wait"`
	ThreadPollMax int           `yaml:"thread_poll_max"`
}

type sendDoc struct {
	MaxBytes int `yaml:"max_bytes"`
	Mentions struct {
		Allow          []string `yaml:"allow"`
		BlockBroadcast *bool    `yaml:"block_broadcast"`
		Max            int      `yaml:"max"`
	} `yaml:"mentions"`
	ContentFilters        []string `yaml:"content_filters"`
	ClassificationMarkers []string `yaml:"classification_markers"`
	LinkAllowlist         []string `yaml:"link_allowlist"`
	Rate                  struct {
		PerMinute int `yaml:"per_minute"`
		PerHour   int `yaml:"per_hour"`
	} `yaml:"rate"`
	ReplyDepthMax int           `yaml:"reply_depth_max"`
	ReplyWindow   time.Duration `yaml:"reply_window"`
	Prefix        string        `yaml:"prefix"`
	MarkerScan    bool          `yaml:"marker_scan"`
}

type limitsDoc struct {
	MaxResults      int  `yaml:"max_results"`
	MaxWritesPerRun *int `yaml:"max_writes_per_run"`
	MaxChatScan     int  `yaml:"max_chat_scan"`
}

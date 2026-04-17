package access

import "time"

// --- Policy Types ---

// Policy stores the local access policy.
type Policy struct {
	AllowedUserIDs      []string
	AllowedChannelIDs   []string
	AllowedRoleIDs      []string
	NoMentionChannelIDs []string
	RequireMention      bool
	Pairing             PairingPolicy
}

// PairingPolicy stores DM pairing limits.
type PairingPolicy struct {
	Enabled     bool
	CodeTTL     time.Duration
	MaxPending  int
	ResendLimit int
}

// MessageContext stores the message fields used for access checks.
type MessageContext struct {
	ChannelID          string
	EffectiveChannelID string
	UserID             string
	RoleIDs            []string
	IsDM               bool
	MentionedBot       bool
	RepliedToBot       bool
}

// --- Decision Types ---

// Action names an access decision action.
type Action string

const (
	ActionAllow Action = "allow"
	ActionDrop  Action = "drop"
	ActionPair  Action = "pair"
)

// Decision stores the result of an access evaluation.
type Decision struct {
	Action      Action
	Reason      string
	PairingCode string
	IsResend    bool
}

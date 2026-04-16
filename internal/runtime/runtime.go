package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/access"
	"github.com/alxxpersonal/exo-discord/internal/audit"
	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/alxxpersonal/exo-discord/internal/hook"
)

// --- Constants ---

const discordMessageLimit = 2000

// --- Types ---

// Auditor writes audit records for runtime events.
type Auditor interface {
	Write(audit.Record) error
}

// Service runs the Discord event loop and hook flow.
type Service struct {
	session     discord.Session
	access      *access.Manager
	hook        hook.Client
	auditor     Auditor
	logger      *slog.Logger
	now         func() time.Time
	newEventID  func() string
	hookTimeout time.Duration
	status      discord.StatusRequest
}

// --- Constructors ---

// New creates a runtime service.
func New(
	session discord.Session,
	manager *access.Manager,
	hookClient hook.Client,
	auditor Auditor,
	logger *slog.Logger,
	hookTimeout time.Duration,
	status discord.StatusRequest,
) *Service {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &Service{
		session:     session,
		access:      manager,
		hook:        hookClient,
		auditor:     auditor,
		logger:      logger,
		now:         time.Now,
		newEventID:  newEventID,
		hookTimeout: hookTimeout,
		status:      status,
	}
}

// --- Runtime ---

// Run opens the session, subscribes to messages, and blocks until shutdown.
func (s *Service) Run(ctx context.Context) error {
	unsubscribe := s.session.Subscribe(func(handlerCtx context.Context, message discord.Message) {
		if err := s.HandleMessage(handlerCtx, message); err != nil {
			s.logger.Error(
				"failed to handle inbound message",
				"component", "runtime",
				"message_id", message.ID,
				"channel_id", message.ChannelID,
				"user_id", message.AuthorID,
				"error", err,
			)
		}
	})
	defer unsubscribe()

	if err := s.session.Open(ctx); err != nil {
		return fmt.Errorf("failed to open discord session: %w", err)
	}
	defer func() {
		if err := s.session.Close(context.Background()); err != nil {
			s.logger.Error("failed to close discord session", "component", "runtime", "error", err)
		}
	}()

	if s.status.Presence != "" {
		if err := s.session.SetStatus(ctx, s.status); err != nil {
			return fmt.Errorf("failed to set initial status: %w", err)
		}
	}

	<-ctx.Done()
	if err := ctx.Err(); err != nil && !isContextClosed(err) {
		return err
	}
	return nil
}

// HandleMessage evaluates access and processes one inbound message.
func (s *Service) HandleMessage(ctx context.Context, message discord.Message) error {
	effectiveChannelID := message.ChannelID
	if message.ThreadParentID != "" {
		effectiveChannelID = message.ThreadParentID
	}

	decision, err := s.access.Evaluate(access.MessageContext{
		ChannelID:          message.ChannelID,
		EffectiveChannelID: effectiveChannelID,
		UserID:             message.AuthorID,
		RoleIDs:            append([]string(nil), message.RoleIDs...),
		IsDM:               message.ChannelKind == discord.ChannelKindDM,
		MentionedBot:       message.MentionedBot,
		RepliedToBot:       message.RepliedToBot,
	})
	if err != nil {
		return fmt.Errorf("failed to evaluate access policy: %w", err)
	}

	switch decision.Action {
	case access.ActionDrop:
		s.record(ctx, audit.Record{
			Timestamp: s.now().UTC(),
			Mode:      s.session.Mode(),
			Event:     "message_denied",
			MessageID: message.ID,
			ChannelID: message.ChannelID,
			UserID:    message.AuthorID,
			Error:     decision.Reason,
		})
		return nil
	case access.ActionPair:
		return s.handlePairing(ctx, message, decision)
	case access.ActionAllow:
		return s.handleAllowed(ctx, message, effectiveChannelID, decision)
	default:
		return fmt.Errorf("unsupported access action %q", decision.Action)
	}
}

// --- Message Handlers ---

func (s *Service) handlePairing(ctx context.Context, message discord.Message, decision access.Decision) error {
	text := pairingMessage(decision.PairingCode, decision.IsResend)
	if _, err := s.session.SendMessage(ctx, discord.SendRequest{
		ChannelID: message.ChannelID,
		Text:      text,
	}); err != nil {
		return fmt.Errorf("failed to send pairing message: %w", err)
	}

	s.record(ctx, audit.Record{
		Timestamp: s.now().UTC(),
		Mode:      s.session.Mode(),
		Event:     "pair_requested",
		MessageID: message.ID,
		ChannelID: message.ChannelID,
		UserID:    message.AuthorID,
		Decision:  string(decision.Action),
		Error:     decision.Reason,
	})
	return nil
}

func (s *Service) handleAllowed(ctx context.Context, message discord.Message, effectiveChannelID string, accessDecision access.Decision) error {
	s.record(ctx, audit.Record{
		Timestamp: s.now().UTC(),
		Mode:      s.session.Mode(),
		Event:     "message_received",
		MessageID: message.ID,
		ChannelID: message.ChannelID,
		UserID:    message.AuthorID,
		Fields: map[string]string{
			"reason": accessDecision.Reason,
		},
	})

	if s.hook == nil {
		return nil
	}

	hookCtx := ctx
	var cancel context.CancelFunc
	if s.hookTimeout > 0 {
		hookCtx, cancel = context.WithTimeout(ctx, s.hookTimeout)
		defer cancel()
	}

	envelope := hook.NewEnvelope(
		s.newEventID(),
		s.session.Mode(),
		message,
		accessDecision.Reason,
		effectiveChannelID,
		s.now(),
	)
	response, err := s.hook.Decide(hookCtx, envelope)
	if err != nil {
		s.logger.Error(
			"hook request failed",
			"component", "runtime",
			"message_id", message.ID,
			"channel_id", message.ChannelID,
			"user_id", message.AuthorID,
			"error", err,
		)
		s.record(ctx, audit.Record{
			Timestamp: s.now().UTC(),
			Mode:      s.session.Mode(),
			Event:     "hook_failed",
			MessageID: message.ID,
			ChannelID: message.ChannelID,
			UserID:    message.AuthorID,
			Error:     err.Error(),
		})
		return nil
	}

	s.record(ctx, audit.Record{
		Timestamp: s.now().UTC(),
		Mode:      s.session.Mode(),
		Event:     "hook_decision",
		MessageID: message.ID,
		ChannelID: message.ChannelID,
		UserID:    message.AuthorID,
		Decision:  string(response.Decision),
		Fields: map[string]string{
			"reason": accessDecision.Reason,
		},
	})

	if err := s.applyResponse(ctx, message, response); err != nil {
		return fmt.Errorf("failed to apply hook decision: %w", err)
	}
	return nil
}

// --- Response Helpers ---

func (s *Service) applyResponse(ctx context.Context, message discord.Message, response hook.Response) error {
	switch response.Decision {
	case hook.DecisionSkip, hook.DecisionDefer:
		return nil
	case hook.DecisionReply:
		return s.applyReply(ctx, message, response.Reply)
	case hook.DecisionReact:
		return s.session.React(ctx, discord.ReactRequest{
			ChannelID: firstNonEmpty(response.React.ChannelID, message.ChannelID),
			MessageID: firstNonEmpty(response.React.MessageID, message.ID),
			Emoji:     response.React.Emoji,
		})
	case hook.DecisionEdit:
		_, err := s.session.EditMessage(ctx, discord.EditRequest{
			ChannelID: firstNonEmpty(response.Edit.ChannelID, message.ChannelID),
			MessageID: response.Edit.MessageID,
			Text:      response.Edit.Text,
		})
		return err
	case hook.DecisionSetStatus:
		return s.session.SetStatus(ctx, discord.StatusRequest{
			Presence:     response.SetStatus.Presence,
			ActivityType: response.SetStatus.ActivityType,
			ActivityText: response.SetStatus.ActivityText,
		})
	default:
		return fmt.Errorf("unsupported hook decision %q", response.Decision)
	}
}

func (s *Service) applyReply(ctx context.Context, message discord.Message, action *hook.ReplyAction) error {
	chunks := chunkContent(action.Text, discordMessageLimit)
	if len(chunks) == 0 {
		return fmt.Errorf("reply text is required")
	}

	replyToID := action.ReplyToMessageID
	if replyToID == "" {
		replyToID = message.ID
	}

	if _, err := s.session.Reply(ctx, discord.ReplyRequest{
		ChannelID:        message.ChannelID,
		GuildID:          message.GuildID,
		Text:             chunks[0],
		ReplyToMessageID: replyToID,
		Files:            append([]string(nil), action.Files...),
	}); err != nil {
		return err
	}

	for _, chunk := range chunks[1:] {
		if _, err := s.session.SendMessage(ctx, discord.SendRequest{
			ChannelID: message.ChannelID,
			Text:      chunk,
		}); err != nil {
			return err
		}
	}

	return nil
}

// --- Audit Helpers ---

func (s *Service) record(_ context.Context, record audit.Record) {
	if s.auditor == nil {
		return
	}
	if err := s.auditor.Write(record); err != nil {
		s.logger.Warn("failed to write audit record", "component", "runtime", "event", record.Event, "error", err)
	}
}

// --- Content Helpers ---

func pairingMessage(code string, resend bool) string {
	if resend {
		return fmt.Sprintf("access is still pending. ask the operator to approve pairing code %s from the local terminal.", code)
	}
	return fmt.Sprintf("access requires local approval. ask the operator to approve pairing code %s from the local terminal.", code)
}

func chunkContent(text string, limit int) []string {
	if text == "" {
		return nil
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}

	chunks := make([]string, 0, (len(runes)/limit)+1)
	for len(runes) > 0 {
		if len(runes) <= limit {
			chunks = append(chunks, string(runes))
			break
		}

		split := bestSplit(runes, limit)
		chunks = append(chunks, string(runes[:split]))
		runes = runes[split:]
	}
	return chunks
}

// --- Split Helpers ---

func bestSplit(runes []rune, limit int) int {
	for idx := limit - 1; idx >= 1; idx-- {
		if runes[idx-1] == '\n' && runes[idx] == '\n' {
			return idx + 1
		}
	}
	for idx := limit - 1; idx >= 0; idx-- {
		if runes[idx] == '\n' {
			return idx + 1
		}
	}
	return limit
}

// --- Value Helpers ---

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// --- Event Helpers ---

func newEventID() string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "evt_" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
	}
	return "evt_" + hex.EncodeToString(random[:])
}

// --- Context Helpers ---

func isContextClosed(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

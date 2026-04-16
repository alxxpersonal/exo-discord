package discord

import "context"

// --- Session Interface ---

// Session defines the Discord operations the runtime uses.
type Session interface {
	Open(context.Context) error
	Close(context.Context) error
	Mode() string
	SendMessage(context.Context, SendRequest) (SentMessage, error)
	Reply(context.Context, ReplyRequest) (SentMessage, error)
	React(context.Context, ReactRequest) error
	EditMessage(context.Context, EditRequest) (SentMessage, error)
	FetchHistory(context.Context, HistoryRequest) ([]Message, error)
	DownloadAttachments(context.Context, DownloadRequest) ([]DownloadedFile, error)
	SetStatus(context.Context, StatusRequest) error
	Subscribe(InboundHandler) func()
}

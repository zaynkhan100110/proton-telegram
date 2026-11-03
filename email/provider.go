package email

import "context"

// Provider is the common interface implemented by every
// email provider.
type Provider interface {
	// Account returns information about the connected account.
	Account() Account

	// SearchMessages searches a mailbox and returns up to limit
	// complete messages.
	SearchMessages(
		ctx context.Context,
		mailbox Mailbox,
		query string,
		limit int,
	) ([]Message, error)

	// GetMessage retrieves one complete email.
	GetMessage(
		ctx context.Context,
		id string,
	) (Message, error)

	// MarkRead marks a message as read.
	MarkRead(
		ctx context.Context,
		id string,
	) error

	// Trash moves a message to Trash.
	//
	// This is intentionally NOT permanent deletion.
	Trash(
		ctx context.Context,
		id string,
	) error

	// Send sends a new email.
	Send(
		ctx context.Context,
		to []string,
		subject string,
		body string,
	) error

	// Reply replies to an existing message.
	Reply(
		ctx context.Context,
		messageID string,
		body string,
	) error
}

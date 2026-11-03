package email

import "time"

// Account represents one connected email account.
type Account struct {
	ID       string
	Provider string
	Address  string
	Name     string
	Enabled  bool
}

// Message represents a complete email message.
type Message struct {
	ID        string
	AccountID string
	ThreadID  string

	From string
	To   []string
	CC   []string
	BCC  []string

	Subject string
	Date    time.Time

	// Complete message bodies.
	BodyText string
	BodyHTML string

	// Message state.
	Read      bool
	Starred   bool
	Important bool

	// Gmail category:
	// Primary, Promotions, Social, Updates, Forums.
	Category string

	Attachments []Attachment
}

// Attachment contains attachment metadata.
// The actual file contents are downloaded separately.
type Attachment struct {
	ID       string
	Name     string
	MimeType string
	Size     int64
}

// Mailbox represents a logical mailbox/folder.
type Mailbox string

const (
	MailboxInbox     Mailbox = "inbox"
	MailboxStarred   Mailbox = "starred"
	MailboxSnoozed   Mailbox = "snoozed"
	MailboxSent      Mailbox = "sent"
	MailboxDraft     Mailbox = "drafts"
	MailboxPurchases Mailbox = "purchases"
	MailboxImportant Mailbox = "important"
	MailboxScheduled Mailbox = "scheduled"
	MailboxAllMail   Mailbox = "all"
	MailboxSpam      Mailbox = "spam"
	MailboxTrash     Mailbox = "trash"
)

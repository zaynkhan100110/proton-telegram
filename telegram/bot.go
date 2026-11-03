package telegram

import (
	"context"
	"fmt"
	"html"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"proton-telegram/email"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type composeStage int

const (
	composeNone composeStage = iota
	composeRecipient
	composeSubject
	composeBody
	composeConfirm
)

type composeSession struct {
	Stage composeStage

	To      []string
	Subject string
	Body    string
}

type Bot struct {
	client       *tgbotapi.BotAPI
	emailService *email.Service

	mu sync.RWMutex

	activeAccounts   map[int64]string
	pendingMailboxes map[int64]email.Mailbox
	pendingDelete    map[int64]bool
	lastMessages     map[int64][]email.Message

	composeSessions map[int64]*composeSession
}

type mailboxCommand struct {
	Name    string
	Mailbox email.Mailbox
	Icon    string
}

var mailboxCommands = map[string]mailboxCommand{
	"/inbox": {
		Name:    "Inbox",
		Mailbox: email.MailboxInbox,
		Icon:    "📥",
	},

	"/starred": {
		Name:    "Starred",
		Mailbox: email.MailboxStarred,
		Icon:    "⭐",
	},

	"/snoozed": {
		Name:    "Snoozed",
		Mailbox: email.MailboxSnoozed,
		Icon:    "😴",
	},

	"/sent": {
		Name:    "Sent",
		Mailbox: email.MailboxSent,
		Icon:    "📤",
	},

	"/drafts": {
		Name:    "Drafts",
		Mailbox: email.MailboxDraft,
		Icon:    "📝",
	},

	"/purchases": {
		Name:    "Purchases",
		Mailbox: email.MailboxPurchases,
		Icon:    "🛒",
	},

	"/important": {
		Name:    "Important",
		Mailbox: email.MailboxImportant,
		Icon:    "❗",
	},

	"/scheduled": {
		Name:    "Scheduled",
		Mailbox: email.MailboxScheduled,
		Icon:    "⏰",
	},

	"/allmail": {
		Name:    "All Mail",
		Mailbox: email.MailboxAllMail,
		Icon:    "📨",
	},

	"/spam": {
		Name:    "Spam",
		Mailbox: email.MailboxSpam,
		Icon:    "🚫",
	},

	"/trash": {
		Name:    "Trash",
		Mailbox: email.MailboxTrash,
		Icon:    "🗑",
	},
}

func NewBot(
	token string,
	emailService *email.Service,
) (*Bot, error) {

	if token == "" {
		return nil, fmt.Errorf(
			"telegram bot token is empty",
		)
	}

	if emailService == nil {
		return nil, fmt.Errorf(
			"email service is nil",
		)
	}

	client, err := tgbotapi.NewBotAPI(
		token,
	)

	if err != nil {
		return nil, err
	}

	log.Printf(
		"Logged in as @%s",
		client.Self.UserName,
	)

	return &Bot{
		client:       client,
		emailService: emailService,

		activeAccounts: make(
			map[int64]string,
		),

		pendingMailboxes: make(
			map[int64]email.Mailbox,
		),

		pendingDelete: make(
			map[int64]bool,
		),

		lastMessages: make(
			map[int64][]email.Message,
		),

		composeSessions: make(
			map[int64]*composeSession,
		),
	}, nil
}

func (b *Bot) Start() {

	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60

	updates := b.client.GetUpdatesChan(
		updateConfig,
	)

	for update := range updates {

		if update.CallbackQuery != nil {
			b.handleCallback(
				update.CallbackQuery,
			)

			continue
		}

		if update.Message == nil {
			continue
		}

		b.handleMessage(
			update.Message,
		)
	}
}

func (b *Bot) handleMessage(
	message *tgbotapi.Message,
) {

	chatID := message.Chat.ID

	text := strings.TrimSpace(
		message.Text,
	)

	if text == "" {
		return
	}

	switch text {

	case "/start":
		b.handleStart(chatID)
		return

	case "/help":
		b.handleHelp(chatID)
		return

	case "/accounts":
		b.handleAccounts(chatID)
		return

	case "/status":
		b.handleStatus(chatID)
		return

	case "/search":
		b.handleSearch(chatID, "")
		return

	case "/compose":
		b.handleCompose(chatID)
		return

	case "/cancel":
		b.handleCancel(chatID)
		return

	case "/delete":
		b.handleDeleteRequest(chatID)
		return

	case "/markread":
		b.handleMarkRead(chatID, "")
		return
	}

	/*
		Compose input must be checked before mailbox input.
	*/
	if strings.HasPrefix(text, "/search ") {
		b.handleSearch(chatID, strings.TrimSpace(strings.TrimPrefix(text, "/search ")))
		return
	}

	if strings.HasPrefix(text, "/markread ") {
		b.handleMarkRead(chatID, strings.TrimSpace(strings.TrimPrefix(text, "/markread ")))
		return
	}

	if b.hasComposeSession(chatID) {

		b.handleComposeInput(
			chatID,
			text,
		)

		return
	}

	/*
		Delete selection must be checked before mailbox count.
	*/
	if b.isWaitingForDelete(chatID) {

		b.handleDeleteNumber(
			chatID,
			text,
		)

		return
	}

	/*
		Mailbox count is checked after command handling.
	*/
	if b.isWaitingForCount(chatID) {

		b.handleEmailCount(
			chatID,
			text,
		)

		return
	}

	if command, ok := mailboxCommands[text]; ok {

		b.handleMailboxCommand(
			chatID,
			command,
		)

		return
	}

	b.send(
		chatID,
		fmt.Sprintf(
			"I received: %s\n\n"+
				"Use /help to see available commands.",
			text,
		),
	)
}

func (b *Bot) handleStart(
	chatID int64,
) {

	providers := b.emailService.Providers()

	if len(providers) == 0 {

		b.send(
			chatID,
			"👋 Welcome!\n\n"+
				"📭 No email accounts are connected yet.\n\n"+
				"Connect an email account first.",
		)

		return
	}

	b.send(
		chatID,
		"👋 Welcome back!\n\n"+
			"📬 Select the email account you want to use.",
	)

	b.handleAccounts(chatID)
}

func (b *Bot) handleHelp(
	chatID int64,
) {

	accountID, selected := b.activeAccount(
		chatID,
	)

	var builder strings.Builder

	builder.WriteString(
		"📧 Email Assistant\n\n",
	)

	if selected {

		provider, err := b.emailService.Provider(
			accountID,
		)

		if err == nil {

			account := provider.Account()

			builder.WriteString(
				fmt.Sprintf(
					"👤 Active account: %s\n\n",
					account.Address,
				),
			)
		}

	} else {

		builder.WriteString(
			"⚠️ No account selected.\n" +
				"Use /accounts first.\n\n",
		)
	}

	builder.WriteString(
		"📂 Mailboxes\n\n",
	)

	builder.WriteString("/inbox\n")
	builder.WriteString("/starred\n")
	builder.WriteString("/snoozed\n")
	builder.WriteString("/sent\n")
	builder.WriteString("/drafts\n")
	builder.WriteString("/purchases\n")
	builder.WriteString("/important\n")
	builder.WriteString("/scheduled\n")
	builder.WriteString("/allmail\n")
	builder.WriteString("/spam\n")
	builder.WriteString("/trash\n\n")

	builder.WriteString(
		"✉️ Actions\n\n",
	)

	builder.WriteString(
		"/compose - compose email\n",
	)

	builder.WriteString(
		"/delete - move fetched email to Trash\n",
	)
	builder.WriteString("/search <words> - search the inbox\n")
	builder.WriteString("/markread <number> - mark a fetched email as read\n")
	builder.WriteString("/status - show connected accounts\n")

	builder.WriteString(
		"/accounts - select account\n",
	)

	builder.WriteString(
		"/cancel - cancel current operation\n",
	)

	builder.WriteString(
		"/help",
	)

	b.send(
		chatID,
		builder.String(),
	)
}

// handleStatus reports provider connectivity without exposing credentials.
func (b *Bot) handleStatus(chatID int64) {
	providers := b.emailService.Providers()
	if len(providers) == 0 {
		b.send(chatID, "No email accounts are connected.")
		return
	}
	var out strings.Builder
	out.WriteString("🔌 Connected email accounts\n\n")
	for _, provider := range providers {
		a := provider.Account()
		marker := "⚪"
		if a.Enabled {
			marker = "🟢"
		}
		out.WriteString(fmt.Sprintf("%s %s — %s\n", marker, strings.ToUpper(a.Provider), a.Address))
	}
	if id, ok := b.activeAccount(chatID); ok {
		if p, err := b.emailService.Provider(id); err == nil {
			out.WriteString("\nActive: " + p.Account().Address)
		}
	} else {
		out.WriteString("\nNo account selected. Use /accounts.")
	}
	b.send(chatID, out.String())
}

// /search searches the selected account's inbox and remembers result numbers
// for follow-up actions such as /markread and /delete.
func (b *Bot) handleSearch(chatID int64, query string) {
	if query == "" {
		b.send(chatID, "Usage: /search words to find\n\nSearches the selected account's inbox.")
		return
	}
	accountID, ok := b.activeAccount(chatID)
	if !ok {
		b.send(chatID, "Select an account first with /accounts.")
		return
	}
	b.send(chatID, "🔎 Searching inbox for: "+query)
	messages, err := b.emailService.SearchMessages(context.Background(), accountID, email.MailboxInbox, query, 10)
	if err != nil {
		b.send(chatID, "Search failed: "+err.Error())
		return
	}
	if len(messages) == 0 {
		b.send(chatID, "No matching messages found.")
		return
	}
	b.mu.Lock()
	b.lastMessages[chatID] = append([]email.Message(nil), messages...)
	b.mu.Unlock()
	command := mailboxCommands["/inbox"]
	for i, message := range messages {
		b.sendFullEmail(chatID, i+1, command, message)
	}
	b.send(chatID, "Use /markread 1 or /delete to act on a message from these results.")
}

// /markread without a number displays usage; a number applies to the most
// recently fetched or searched message list.
func (b *Bot) handleMarkRead(chatID int64, value string) {
	if value == "" {
		b.send(chatID, "Usage: /markread 2\n\nChoose a number from your most recently fetched email list.")
		return
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		b.send(chatID, "Please provide a valid email number, for example /markread 2.")
		return
	}
	b.mu.RLock()
	messages := b.lastMessages[chatID]
	b.mu.RUnlock()
	if number < 1 || number > len(messages) {
		b.send(chatID, fmt.Sprintf("Choose a number between 1 and %d.", len(messages)))
		return
	}
	accountID, ok := b.activeAccount(chatID)
	if !ok {
		b.send(chatID, "No active email account. Use /accounts.")
		return
	}
	message := messages[number-1]
	if err := b.emailService.MarkRead(context.Background(), accountID, message.ID); err != nil {
		b.send(chatID, "Could not mark the message as read: "+err.Error())
		return
	}
	b.send(chatID, fmt.Sprintf("✅ Email %d marked as read.\nSubject: %s", number, message.Subject))
}

func (b *Bot) handleAccounts(
	chatID int64,
) {

	providers := b.emailService.Providers()

	if len(providers) == 0 {

		b.send(
			chatID,
			"📭 No email accounts connected.",
		)

		return
	}

	keyboard := make(
		[][]tgbotapi.InlineKeyboardButton,
		0,
		len(providers),
	)

	for _, provider := range providers {

		account := provider.Account()

		if !account.Enabled {
			continue
		}

		button := tgbotapi.NewInlineKeyboardButtonData(
			account.Address,
			"account:"+account.ID,
		)

		keyboard = append(
			keyboard,
			tgbotapi.NewInlineKeyboardRow(
				button,
			),
		)
	}

	if len(keyboard) == 0 {

		b.send(
			chatID,
			"📭 No enabled email accounts.",
		)

		return
	}

	msg := tgbotapi.NewMessage(
		chatID,
		"📬 Select an email account:",
	)

	msg.ReplyMarkup = tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: keyboard,
	}

	if _, err := b.client.Send(msg); err != nil {

		log.Printf(
			"failed to send account keyboard: %v",
			err,
		)
	}
}

func (b *Bot) handleCallback(
	query *tgbotapi.CallbackQuery,
) {

	if query == nil ||
		query.Message == nil {

		return
	}

	chatID := query.Message.Chat.ID

	data := query.Data

	if !strings.HasPrefix(
		data,
		"account:",
	) {

		b.answerCallback(
			query.ID,
			"Unknown action.",
		)

		return
	}

	accountID := strings.TrimPrefix(
		data,
		"account:",
	)

	provider, err := b.emailService.Provider(
		accountID,
	)

	if err != nil {

		b.answerCallback(
			query.ID,
			"Account not found.",
		)

		return
	}

	account := provider.Account()

	b.mu.Lock()

	b.activeAccounts[chatID] = account.ID

	delete(
		b.pendingMailboxes,
		chatID,
	)

	delete(
		b.pendingDelete,
		chatID,
	)

	delete(
		b.lastMessages,
		chatID,
	)

	b.mu.Unlock()

	b.answerCallback(
		query.ID,
		"Account selected.",
	)

	b.send(
		chatID,
		fmt.Sprintf(
			"✅ Account selected:\n%s\n\n"+
				"Now select a mailbox:\n\n"+
				"/inbox\n"+
				"/starred\n"+
				"/snoozed\n"+
				"/sent\n"+
				"/drafts\n"+
				"/purchases\n"+
				"/important\n"+
				"/scheduled\n"+
				"/allmail\n"+
				"/spam\n"+
				"/trash",
			account.Address,
		),
	)
}

func (b *Bot) answerCallback(
	callbackID string,
	text string,
) {

	if callbackID == "" {
		return
	}

	callback := tgbotapi.NewCallback(
		callbackID,
		text,
	)

	if _, err := b.client.Request(
		callback,
	); err != nil {

		log.Printf(
			"failed to answer callback: %v",
			err,
		)
	}
}

func (b *Bot) activeAccount(
	chatID int64,
) (string, bool) {

	b.mu.RLock()
	defer b.mu.RUnlock()

	accountID, ok := b.activeAccounts[chatID]

	return accountID, ok
}

func (b *Bot) handleMailboxCommand(
	chatID int64,
	command mailboxCommand,
) {

	_, ok := b.activeAccount(
		chatID,
	)

	if !ok {

		b.send(
			chatID,
			"⚠️ No email account selected.\n\n"+
				"Use /accounts first.",
		)

		return
	}

	b.mu.Lock()

	b.pendingMailboxes[chatID] =
		command.Mailbox

	b.mu.Unlock()

	b.send(
		chatID,
		fmt.Sprintf(
			"%s %s selected.\n\n"+
				"How many emails do you want to fetch?\n\n"+
				"Reply with a number, for example:\n"+
				"5",
			command.Icon,
			command.Name,
		),
	)
}

func (b *Bot) isWaitingForCount(
	chatID int64,
) bool {

	b.mu.RLock()
	defer b.mu.RUnlock()

	_, ok := b.pendingMailboxes[chatID]

	return ok
}

func (b *Bot) handleEmailCount(
	chatID int64,
	text string,
) {

	count, err := strconv.Atoi(
		strings.TrimSpace(text),
	)

	if err != nil ||
		count <= 0 {

		b.send(
			chatID,
			"❌ Please enter a valid positive number.\n\n"+
				"Example: 5",
		)

		return
	}

	if count > 100 {

		b.send(
			chatID,
			"❌ Maximum is 100 emails per request.",
		)

		return
	}

	b.mu.Lock()

	mailbox := b.pendingMailboxes[chatID]

	delete(
		b.pendingMailboxes,
		chatID,
	)

	b.mu.Unlock()

	accountID, ok := b.activeAccount(
		chatID,
	)

	if !ok {

		b.send(
			chatID,
			"❌ No active email account.",
		)

		return
	}

	provider, err := b.emailService.Provider(
		accountID,
	)

	if err != nil {

		b.send(
			chatID,
			fmt.Sprintf(
				"❌ Email account unavailable:\n%s",
				err,
			),
		)

		return
	}

	account := provider.Account()

	command := mailboxCommandFor(
		mailbox,
	)

	b.send(
		chatID,
		fmt.Sprintf(
			"🔎 Fetching %d email(s) from %s for %s...",
			count,
			command.Name,
			account.Address,
		),
	)

	ctx := context.Background()

	/*
		Fetch one extra message so we can tell whether
		the mailbox contains fewer messages than requested.
	*/
	messages, err := b.emailService.SearchMessages(
		ctx,
		accountID,
		mailbox,
		"",
		count+1,
	)

	if err != nil {

		log.Printf(
			"failed to fetch messages: %v",
			err,
		)

		b.send(
			chatID,
			fmt.Sprintf(
				"❌ Failed to fetch emails:\n%s",
				err,
			),
		)

		return
	}

	available := len(messages)

	if available > count {

		messages = messages[:count]

		available = count
	}

	if available < count {

		b.send(
			chatID,
			fmt.Sprintf(
				"ℹ️ You requested %d email(s), "+
					"but only %d email(s) are available in %s.\n\n"+
					"I will fetch all %d available email(s).",
				count,
				available,
				command.Name,
				available,
			),
		)
	}

	if available == 0 {

		b.send(
			chatID,
			fmt.Sprintf(
				"📭 %s is empty.",
				command.Name,
			),
		)

		return
	}

	b.mu.Lock()

	b.lastMessages[chatID] = append(
		[]email.Message(nil),
		messages...,
	)

	b.mu.Unlock()

	/*
		Each email is completely prepared and sent
		before we move to the next email.

		If one email is larger than Telegram's limit,
		sendEmailParts() sends all of its parts first.
	*/
	for i, message := range messages {

		b.sendFullEmail(
			chatID,
			i+1,
			command,
			message,
		)
	}
}

func mailboxCommandFor(
	mailbox email.Mailbox,
) mailboxCommand {

	for _, command := range mailboxCommands {

		if command.Mailbox == mailbox {
			return command
		}
	}

	return mailboxCommand{
		Name:    string(mailbox),
		Mailbox: mailbox,
	}
}

func (b *Bot) sendFullEmail(
	chatID int64,
	number int,
	command mailboxCommand,
	message email.Message,
) {

	header := formatEmailHeader(
		number,
		command,
		message,
	)

	body := emailBodyForTelegram(
		message,
	)

	fullEmail :=
		header +
			"\n" +
			body

	b.sendEmailParts(
		chatID,
		fullEmail,
	)
}

func formatEmailHeader(
	number int,
	command mailboxCommand,
	message email.Message,
) string {

	var builder strings.Builder

	subject := strings.TrimSpace(
		message.Subject,
	)

	if subject == "" {
		subject = "(no subject)"
	}

	builder.WriteString(
		fmt.Sprintf(
			"📧 Email %d\n",
			number,
		),
	)

	builder.WriteString(
		fmt.Sprintf(
			"📂 Mailbox: %s\n",
			command.Name,
		),
	)

	if message.Category != "" {

		builder.WriteString(
			fmt.Sprintf(
				"🏷 Category: %s\n",
				message.Category,
			),
		)
	}

	if message.Starred {
		builder.WriteString(
			"⭐ Starred\n",
		)
	}

	if message.Important {
		builder.WriteString(
			"❗ Important\n",
		)
	}

	if message.Read {

		builder.WriteString(
			"📖 Read\n",
		)

	} else {

		builder.WriteString(
			"📩 Unread\n",
		)
	}

	if !message.Date.IsZero() {

		builder.WriteString(
			fmt.Sprintf(
				"🕐 Received: %s\n",
				message.Date.Format(
					time.RFC1123Z,
				),
			),
		)
	}

	builder.WriteString(
		fmt.Sprintf(
			"From: %s\n",
			message.From,
		),
	)

	if len(message.To) > 0 {

		builder.WriteString(
			fmt.Sprintf(
				"To: %s\n",
				strings.Join(
					message.To,
					", ",
				),
			),
		)
	}

	if len(message.CC) > 0 {

		builder.WriteString(
			fmt.Sprintf(
				"CC: %s\n",
				strings.Join(
					message.CC,
					", ",
				),
			),
		)
	}

	builder.WriteString(
		fmt.Sprintf(
			"Subject: %s\n",
			subject,
		),
	)

	if len(message.Attachments) > 0 {

		builder.WriteString(
			fmt.Sprintf(
				"📎 Attachments: %d\n",
				len(message.Attachments),
			),
		)

		for _, attachment := range message.Attachments {

			builder.WriteString(
				fmt.Sprintf(
					"   • %s",
					attachment.Name,
				),
			)

			if attachment.Size > 0 {

				builder.WriteString(
					fmt.Sprintf(
						" (%s)",
						formatFileSize(
							attachment.Size,
						),
					),
				)
			}

			builder.WriteString("\n")
		}
	}

	builder.WriteString(
		"\n━━━━━━━━━━━━━━━━━━━━\n\n",
	)

	return builder.String()
}

// emailBodyForTelegram chooses the best readable body.
func emailBodyForTelegram(
	message email.Message,
) string {

	/*
		Always prefer text/plain.
	*/
	if strings.TrimSpace(
		message.BodyText,
	) != "" {

		return strings.TrimSpace(
			message.BodyText,
		)
	}

	/*
		If the email is HTML-only, convert it to
		readable plain text.
	*/
	if strings.TrimSpace(
		message.BodyHTML,
	) != "" {

		return htmlToPlainText(
			message.BodyHTML,
		)
	}

	return "(empty message body)"
}

// htmlToPlainText converts an HTML email into readable text.
func htmlToPlainText(
	source string,
) string {

	if strings.TrimSpace(source) == "" {
		return ""
	}

	text := source

	scriptPattern := regexp.MustCompile(
		`(?is)<script[^>]*>.*?</script>`,
	)

	text = scriptPattern.ReplaceAllString(
		text,
		"",
	)

	stylePattern := regexp.MustCompile(
		`(?is)<style[^>]*>.*?</style>`,
	)

	text = stylePattern.ReplaceAllString(
		text,
		"",
	)

	/*
		Convert common structural tags into line breaks
		before removing the HTML tags.
	*/
	blockPattern := regexp.MustCompile(
		`(?i)</?(p|div|br|li|tr|table|h1|h2|h3|h4|h5|h6|blockquote)[^>]*>`,
	)

	text = blockPattern.ReplaceAllString(
		text,
		"\n",
	)

	tagPattern := regexp.MustCompile(
		`(?s)<[^>]*>`,
	)

	text = tagPattern.ReplaceAllString(
		text,
		"",
	)

	text = html.UnescapeString(
		text,
	)

	text = strings.ReplaceAll(
		text,
		"\r\n",
		"\n",
	)

	text = strings.ReplaceAll(
		text,
		"\r",
		"\n",
	)

	lines := strings.Split(
		text,
		"\n",
	)

	cleanLines := make(
		[]string,
		0,
		len(lines),
	)

	previousBlank := false

	for _, line := range lines {

		line = strings.TrimSpace(
			line,
		)

		if line == "" {

			if previousBlank {
				continue
			}

			previousBlank = true

			cleanLines = append(
				cleanLines,
				"",
			)

			continue
		}

		previousBlank = false

		cleanLines = append(
			cleanLines,
			line,
		)
	}

	return strings.TrimSpace(
		strings.Join(
			cleanLines,
			"\n",
		),
	)
}

func formatFileSize(
	size int64,
) string {

	if size < 1024 {

		return fmt.Sprintf(
			"%d B",
			size,
		)
	}

	if size < 1024*1024 {

		return fmt.Sprintf(
			"%.1f KB",
			float64(size)/1024,
		)
	}

	if size < 1024*1024*1024 {

		return fmt.Sprintf(
			"%.1f MB",
			float64(size)/(1024*1024),
		)
	}

	return fmt.Sprintf(
		"%.1f GB",
		float64(size)/(1024*1024*1024),
	)
}

// sendEmailParts sends one complete email in Telegram-sized
// chunks before the caller proceeds to the next email.
func (b *Bot) sendEmailParts(
	chatID int64,
	text string,
) {

	/*
		Telegram's maximum text message size is larger than this,
		but 3500 gives us some safety room.
	*/
	const maxTelegramMessageLength = 3500

	for len(text) > 0 {

		chunkSize :=
			maxTelegramMessageLength

		if len(text) < chunkSize {
			chunkSize = len(text)
		}

		// Do not split a multi-byte UTF-8 character across Telegram messages.
		if chunkSize < len(text) {
			for chunkSize > 0 && !utf8.RuneStart(text[chunkSize]) {
				chunkSize--
			}
		}
		if chunkSize == 0 {
			_, size := utf8.DecodeRuneInString(text)
			chunkSize = size
		}
		chunk := text[:chunkSize]

		msg := tgbotapi.NewMessage(
			chatID,
			chunk,
		)

		if _, err := b.client.Send(
			msg,
		); err != nil {

			log.Printf(
				"failed to send email part: %v",
				err,
			)

			return
		}

		text = text[chunkSize:]
	}
}

// -----------------------------------------------------------------------------
// DELETE
// -----------------------------------------------------------------------------

func (b *Bot) handleDeleteRequest(
	chatID int64,
) {

	b.mu.RLock()

	messages := b.lastMessages[chatID]

	b.mu.RUnlock()

	if len(messages) == 0 {

		b.send(
			chatID,
			"⚠️ There are no recently fetched emails to delete.\n\n"+
				"First select a mailbox and fetch some emails.",
		)

		return
	}

	b.mu.Lock()

	b.pendingDelete[chatID] = true

	b.mu.Unlock()

	b.send(
		chatID,
		"🗑 Which email do you want to move to Trash?\n\n"+
			"Reply with its number from the last email list.\n\n"+
			"Example:\n"+
			"2\n\n"+
			"Use /cancel to cancel.",
	)
}

func (b *Bot) isWaitingForDelete(
	chatID int64,
) bool {

	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.pendingDelete[chatID]
}

func (b *Bot) handleDeleteNumber(
	chatID int64,
	text string,
) {

	number, err := strconv.Atoi(
		strings.TrimSpace(text),
	)

	if err != nil {

		b.send(
			chatID,
			"❌ Please enter the email number.\n\n"+
				"Example: 2",
		)

		return
	}

	b.mu.Lock()

	delete(
		b.pendingDelete,
		chatID,
	)

	messages := b.lastMessages[chatID]

	b.mu.Unlock()

	if number < 1 ||
		number > len(messages) {

		b.send(
			chatID,
			fmt.Sprintf(
				"❌ Invalid email number. Choose between 1 and %d.",
				len(messages),
			),
		)

		return
	}

	message := messages[number-1]

	accountID, ok := b.activeAccount(
		chatID,
	)

	if !ok {

		b.send(
			chatID,
			"❌ No active email account.",
		)

		return
	}

	b.send(
		chatID,
		fmt.Sprintf(
			"🗑 Moving email %d to Trash...",
			number,
		),
	)

	ctx := context.Background()

	if err := b.emailService.TrashMessage(
		ctx,
		accountID,
		message.ID,
	); err != nil {

		log.Printf(
			"failed to trash email %s: %v",
			message.ID,
			err,
		)

		b.send(
			chatID,
			fmt.Sprintf(
				"❌ Failed to move email to Trash:\n%s",
				err,
			),
		)

		return
	}

	b.mu.Lock()

	remaining := make(
		[]email.Message,
		0,
		len(messages)-1,
	)

	for i, item := range messages {

		if i != number-1 {

			remaining = append(
				remaining,
				item,
			)
		}
	}

	b.lastMessages[chatID] =
		remaining

	b.mu.Unlock()

	b.send(
		chatID,
		fmt.Sprintf(
			"✅ Email %d moved to Trash.\n\n"+
				"Subject: %s",
			number,
			message.Subject,
		),
	)
}

// -----------------------------------------------------------------------------
// COMPOSE
// -----------------------------------------------------------------------------

func (b *Bot) handleCompose(
	chatID int64,
) {

	accountID, ok := b.activeAccount(
		chatID,
	)

	if !ok {

		b.send(
			chatID,
			"⚠️ No email account selected.\n\n"+
				"Select an account first using /accounts.",
		)

		return
	}

	provider, err := b.emailService.Provider(
		accountID,
	)

	if err != nil {

		b.send(
			chatID,
			fmt.Sprintf(
				"❌ Unable to access selected account:\n%s",
				err,
			),
		)

		return
	}

	account := provider.Account()

	session := &composeSession{
		Stage: composeRecipient,
	}

	b.mu.Lock()

	b.composeSessions[chatID] =
		session

	b.mu.Unlock()

	b.send(
		chatID,
		fmt.Sprintf(
			"✉️ Compose\n\n"+
				"From: %s\n\n"+
				"Enter recipient email address.\n\n"+
				"For multiple recipients, separate them with commas.\n\n"+
				"Example:\n"+
				"john@example.com, jane@example.com\n\n"+
				"Use /cancel to cancel.",
			account.Address,
		),
	)
}

func (b *Bot) hasComposeSession(
	chatID int64,
) bool {

	b.mu.RLock()
	defer b.mu.RUnlock()

	_, ok := b.composeSessions[chatID]

	return ok
}

func (b *Bot) handleComposeInput(
	chatID int64,
	text string,
) {

	b.mu.RLock()

	session, ok := b.composeSessions[chatID]

	b.mu.RUnlock()

	if !ok {
		return
	}

	switch session.Stage {

	case composeRecipient:

		b.handleComposeRecipient(
			chatID,
			session,
			text,
		)

	case composeSubject:

		b.handleComposeSubject(
			chatID,
			session,
			text,
		)

	case composeBody:

		b.handleComposeBody(
			chatID,
			session,
			text,
		)

	case composeConfirm:

		b.handleComposeConfirmation(
			chatID,
			session,
			text,
		)

	default:

		b.clearComposeSession(
			chatID,
		)
	}
}

func (b *Bot) handleComposeRecipient(
	chatID int64,
	session *composeSession,
	text string,
) {

	rawRecipients := strings.Split(
		text,
		",",
	)

	recipients := make(
		[]string,
		0,
		len(rawRecipients),
	)

	for _, recipient := range rawRecipients {

		recipient = strings.TrimSpace(
			recipient,
		)

		if recipient == "" {
			continue
		}

		if !strings.Contains(
			recipient,
			"@",
		) {

			b.send(
				chatID,
				"❌ Invalid email address:\n"+
					recipient+
					"\n\n"+
					"Try again.",
			)

			return
		}

		recipients = append(
			recipients,
			recipient,
		)
	}

	if len(recipients) == 0 {

		b.send(
			chatID,
			"❌ Please enter at least one recipient.",
		)

		return
	}

	b.mu.Lock()

	session.To = recipients
	session.Stage = composeSubject

	b.mu.Unlock()

	b.send(
		chatID,
		fmt.Sprintf(
			"✅ Recipient:\n%s\n\n"+
				"Now enter the subject.\n\n"+
				"You can use /cancel at any time.",
			strings.Join(
				recipients,
				", ",
			),
		),
	)
}

func (b *Bot) handleComposeSubject(
	chatID int64,
	session *composeSession,
	text string,
) {

	b.mu.Lock()

	session.Subject =
		strings.TrimSpace(text)

	session.Stage = composeBody

	b.mu.Unlock()

	b.send(
		chatID,
		"📝 Subject saved.\n\n"+
			"Now send the email body.\n\n"+
			"You can send multiple lines in one Telegram message.",
	)
}

func (b *Bot) handleComposeBody(
	chatID int64,
	session *composeSession,
	text string,
) {

	if strings.TrimSpace(text) == "" {

		b.send(
			chatID,
			"❌ The email body cannot be empty.\n\n"+
				"Please enter the message body.",
		)

		return
	}

	b.mu.Lock()

	session.Body = text
	session.Stage = composeConfirm

	b.mu.Unlock()

	b.sendComposePreview(
		chatID,
		session,
	)
}

func (b *Bot) sendComposePreview(
	chatID int64,
	session *composeSession,
) {

	text :=
		"📨 Email Preview\n\n" +
			"To: " +
			strings.Join(
				session.To,
				", ",
			) +
			"\n" +
			"Subject: " +
			session.Subject +
			"\n\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			session.Body +
			"\n\n" +
			"━━━━━━━━━━━━━━━━━━━━\n\n" +
			"Send this email?\n\n" +
			"Reply with:\n" +
			"YES — send\n" +
			"NO — cancel"

	b.send(
		chatID,
		text,
	)
}

func (b *Bot) handleComposeConfirmation(
	chatID int64,
	session *composeSession,
	text string,
) {

	answer := strings.ToLower(
		strings.TrimSpace(text),
	)

	switch answer {

	case "yes", "y":

		b.send(
			chatID,
			"📤 Sending email...",
		)

		accountID, ok :=
			b.activeAccount(chatID)

		if !ok {

			b.clearComposeSession(
				chatID,
			)

			b.send(
				chatID,
				"❌ No active email account.",
			)

			return
		}

		ctx := context.Background()

		err := b.emailService.Send(
			ctx,
			accountID,
			session.To,
			session.Subject,
			session.Body,
		)

		if err != nil {

			log.Printf(
				"failed to send email: %v",
				err,
			)

			b.send(
				chatID,
				fmt.Sprintf(
					"❌ Failed to send email:\n%s",
					err,
				),
			)

			return
		}

		b.clearComposeSession(
			chatID,
		)

		b.send(
			chatID,
			"✅ Email sent successfully.\n\n"+
				"To: "+
				strings.Join(
					session.To,
					", ",
				),
		)

	case "no", "n":

		b.clearComposeSession(
			chatID,
		)

		b.send(
			chatID,
			"❌ Email cancelled.",
		)

	default:

		b.send(
			chatID,
			"Please reply with YES to send or NO to cancel.",
		)
	}
}

func (b *Bot) clearComposeSession(
	chatID int64,
) {

	b.mu.Lock()

	delete(
		b.composeSessions,
		chatID,
	)

	b.mu.Unlock()
}

func (b *Bot) handleCancel(
	chatID int64,
) {

	b.mu.Lock()

	composeActive :=
		b.composeSessions[chatID] != nil

	delete(
		b.composeSessions,
		chatID,
	)

	delete(
		b.pendingMailboxes,
		chatID,
	)

	delete(
		b.pendingDelete,
		chatID,
	)

	b.mu.Unlock()

	if composeActive {

		b.send(
			chatID,
			"❌ Compose cancelled.",
		)

		return
	}

	b.send(
		chatID,
		"❌ Current operation cancelled.",
	)
}

// -----------------------------------------------------------------------------
// GENERAL TELEGRAM SENDING
// -----------------------------------------------------------------------------

func (b *Bot) send(
	chatID int64,
	text string,
) {

	if strings.TrimSpace(text) == "" {
		return
	}

	const maxTelegramMessageLength = 3500

	if len(text) <= maxTelegramMessageLength {

		msg := tgbotapi.NewMessage(
			chatID,
			text,
		)

		if _, err := b.client.Send(
			msg,
		); err != nil {

			log.Printf(
				"failed to send Telegram message: %v",
				err,
			)
		}

		return
	}

	b.sendEmailParts(
		chatID,
		text,
	)
}

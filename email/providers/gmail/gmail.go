package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"proton-telegram/email"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

const (
	credentialsFile = "credentials/gmail-client.json"
	tokenFile       = "credentials/gmail-token.json"
)

type Provider struct {
	service *gmail.Service
	account email.Account
}

// New creates and authenticates the Gmail provider.
func New(ctx context.Context) (*Provider, error) {

	credentials, err := os.ReadFile(
		credentialsFile,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"read Gmail credentials: %w",
			err,
		)
	}

	// GmailModifyScope is required because the bot can now:
	// - mark messages read
	// - move messages to Trash
	// - send messages
	config, err := google.ConfigFromJSON(
		credentials,
		gmail.GmailModifyScope,
	)

	if err != nil {
		return nil, fmt.Errorf(
			"parse Gmail credentials: %w",
			err,
		)
	}

	token, err := loadToken()

	if err != nil {

		fmt.Println()
		fmt.Println("No Gmail token found.")
		fmt.Println("Starting Gmail OAuth authorization...")
		fmt.Println()

		token, err = authorize(
			ctx,
			config,
		)

		if err != nil {
			return nil, err
		}

		if err := saveToken(token); err != nil {
			return nil, err
		}
	}

	client := config.Client(
		ctx,
		token,
	)

	service, err := gmail.NewService(
		ctx,
		option.WithHTTPClient(client),
	)

	if err != nil {
		return nil, fmt.Errorf(
			"create Gmail service: %w",
			err,
		)
	}

	profile, err := service.Users.
		GetProfile("me").
		Do()

	if err != nil {
		return nil, fmt.Errorf(
			"get Gmail profile: %w",
			err,
		)
	}

	account := email.Account{
		ID:       "gmail-" + profile.EmailAddress,
		Provider: "gmail",
		Address:  profile.EmailAddress,
		Name:     profile.EmailAddress,
		Enabled:  true,
	}

	return &Provider{
		service: service,
		account: account,
	}, nil
}

// Account returns this Gmail account.
func (p *Provider) Account() email.Account {
	return p.account
}

// SearchMessages searches a Gmail mailbox.
func (p *Provider) SearchMessages(
	ctx context.Context,
	mailbox email.Mailbox,
	query string,
	limit int,
) ([]email.Message, error) {

	if limit <= 0 {
		return []email.Message{}, nil
	}

	gmailQuery := mailboxQuery(
		mailbox,
	)

	query = strings.TrimSpace(
		query,
	)

	if query != "" {

		if gmailQuery != "" {
			gmailQuery += " "
		}

		gmailQuery += query
	}

	response, err := p.service.Users.Messages.
		List("me").
		Q(gmailQuery).
		MaxResults(int64(limit)).
		Do()

	if err != nil {
		return nil, fmt.Errorf(
			"search Gmail messages: %w",
			err,
		)
	}

	messages := make(
		[]email.Message,
		0,
		len(response.Messages),
	)

	/*
		Gmail List() gives IDs/snippets.

		We intentionally retrieve every selected message
		with Format("full") so Telegram receives the
		complete email.
	*/
	for _, item := range response.Messages {

		if item == nil || item.Id == "" {
			continue
		}

		message, err := p.GetMessage(
			ctx,
			item.Id,
		)

		if err != nil {
			return nil, err
		}

		messages = append(
			messages,
			message,
		)

		if len(messages) >= limit {
			break
		}
	}

	return messages, nil
}

// GetMessage retrieves the complete Gmail message.
func (p *Provider) GetMessage(
	ctx context.Context,
	id string,
) (email.Message, error) {

	id = strings.TrimSpace(id)

	if id == "" {
		return email.Message{}, fmt.Errorf(
			"Gmail message ID is empty",
		)
	}

	message, err := p.service.Users.Messages.
		Get("me", id).
		Format("full").
		Do()

	if err != nil {
		return email.Message{}, fmt.Errorf(
			"get Gmail message: %w",
			err,
		)
	}

	return parseMessage(
		message,
		p.account.ID,
	), nil
}

// MarkRead marks a Gmail message as read.
func (p *Provider) MarkRead(
	ctx context.Context,
	id string,
) error {

	id = strings.TrimSpace(id)

	if id == "" {
		return fmt.Errorf(
			"Gmail message ID is empty",
		)
	}

	_, err := p.service.Users.Messages.
		Modify(
			"me",
			id,
			&gmail.ModifyMessageRequest{
				RemoveLabelIds: []string{
					"UNREAD",
				},
			},
		).
		Do()

	if err != nil {
		return fmt.Errorf(
			"mark Gmail message as read: %w",
			err,
		)
	}

	return nil
}

// Trash moves a Gmail message to Trash.
func (p *Provider) Trash(
	ctx context.Context,
	id string,
) error {

	id = strings.TrimSpace(id)

	if id == "" {
		return fmt.Errorf(
			"Gmail message ID is empty",
		)
	}

	_, err := p.service.Users.Messages.
		Trash(
			"me",
			id,
		).
		Do()

	if err != nil {
		return fmt.Errorf(
			"move Gmail message to trash: %w",
			err,
		)
	}

	return nil
}

// Send sends a plain-text Gmail message.
func (p *Provider) Send(
	ctx context.Context,
	to []string,
	subject string,
	body string,
) error {

	if len(to) == 0 {
		return fmt.Errorf(
			"recipient list is empty",
		)
	}

	recipients := make(
		[]string,
		0,
		len(to),
	)

	for _, recipient := range to {

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
			return fmt.Errorf(
				"invalid recipient: %s",
				recipient,
			)
		}

		recipients = append(
			recipients,
			recipient,
		)
	}

	if len(recipients) == 0 {
		return fmt.Errorf(
			"recipient list is empty",
		)
	}

	subject = strings.TrimSpace(
		subject,
	)

	if subject == "" {
		subject = "(no subject)"
	}

	var raw strings.Builder

	raw.WriteString(
		"To: ",
	)

	raw.WriteString(
		strings.Join(
			recipients,
			", ",
		),
	)

	raw.WriteString("\r\n")

	raw.WriteString(
		"Subject: ",
	)

	raw.WriteString(
		subject,
	)

	raw.WriteString("\r\n")

	raw.WriteString(
		"MIME-Version: 1.0\r\n",
	)

	raw.WriteString(
		"Content-Type: text/plain; charset=UTF-8\r\n",
	)

	raw.WriteString(
		"Content-Transfer-Encoding: 8bit\r\n",
	)

	raw.WriteString("\r\n")

	raw.WriteString(body)

	encoded := base64.RawURLEncoding.EncodeToString(
		[]byte(raw.String()),
	)

	_, err := p.service.Users.Messages.
		Send(
			"me",
			&gmail.Message{
				Raw: encoded,
			},
		).
		Do()

	if err != nil {
		return fmt.Errorf(
			"send Gmail message: %w",
			err,
		)
	}

	return nil
}

// Reply is not implemented yet.
func (p *Provider) Reply(
	ctx context.Context,
	messageID string,
	body string,
) error {

	return fmt.Errorf(
		"Gmail Reply is not implemented yet",
	)
}

// mailboxQuery converts our generic mailbox to Gmail search syntax.
func mailboxQuery(
	mailbox email.Mailbox,
) string {

	switch mailbox {

	case email.MailboxInbox:
		return "in:inbox"

	case email.MailboxStarred:
		return "is:starred"

	case email.MailboxSnoozed:
		return "is:snoozed"

	case email.MailboxSent:
		return "in:sent"

	case email.MailboxDraft:
		return "in:drafts"

	case email.MailboxPurchases:
		return "category:purchases"

	case email.MailboxImportant:
		return "is:important"

	case email.MailboxScheduled:
		return "is:scheduled"

	case email.MailboxAllMail:
		return "-in:spam -in:trash"

	case email.MailboxSpam:
		return "in:spam"

	case email.MailboxTrash:
		return "in:trash"

	default:
		return ""
	}
}

// parseMessage converts Gmail API data into our Message model.
func parseMessage(
	message *gmail.Message,
	accountID string,
) email.Message {

	if message == nil {
		return email.Message{
			AccountID: accountID,
		}
	}

	headers := make(
		map[string]string,
	)

	for _, header := range message.Payload.Headers {

		if header == nil {
			continue
		}

		headers[strings.ToLower(
			header.Name,
		)] = header.Value
	}

	read := true
	starred := false
	important := false
	category := ""

	for _, label := range message.LabelIds {

		switch label {

		case "UNREAD":
			read = false

		case "STARRED":
			starred = true

		case "IMPORTANT":
			important = true

		case "CATEGORY_PRIMARY":
			category = "Primary"

		case "CATEGORY_PROMOTIONS":
			category = "Promotions"

		case "CATEGORY_SOCIAL":
			category = "Social"

		case "CATEGORY_UPDATES":
			category = "Updates"

		case "CATEGORY_FORUMS":
			category = "Forums"
		}
	}

	messageDate := time.Time{}

	if message.InternalDate > 0 {
		messageDate = time.UnixMilli(
			message.InternalDate,
		)
	}

	bodyText, bodyHTML := extractBodies(
		message.Payload,
	)

	return email.Message{
		ID:        message.Id,
		AccountID: accountID,
		ThreadID:  message.ThreadId,

		From: headers["from"],
		To:   splitAddresses(headers["to"]),
		CC:   splitAddresses(headers["cc"]),
		BCC:  splitAddresses(headers["bcc"]),

		Subject: headers["subject"],
		Date:    messageDate,

		BodyText: bodyText,
		BodyHTML: bodyHTML,

		Read:      read,
		Starred:   starred,
		Important: important,
		Category:  category,

		Attachments: extractAttachments(
			message.Payload,
		),
	}
}

// extractBodies recursively extracts text/plain and text/html.
func extractBodies(
	payload *gmail.MessagePart,
) (string, string) {

	if payload == nil {
		return "", ""
	}

	var textBody string
	var htmlBody string

	if payload.MimeType == "text/plain" &&
		payload.Body != nil &&
		payload.Body.Data != "" {

		textBody = decodeBody(
			payload.Body.Data,
		)
	}

	if payload.MimeType == "text/html" &&
		payload.Body != nil &&
		payload.Body.Data != "" {

		htmlBody = decodeBody(
			payload.Body.Data,
		)
	}

	for _, part := range payload.Parts {

		childText, childHTML := extractBodies(
			part,
		)

		if textBody == "" &&
			childText != "" {

			textBody = childText
		}

		if htmlBody == "" &&
			childHTML != "" {

			htmlBody = childHTML
		}

		if textBody != "" &&
			htmlBody != "" {

			break
		}
	}

	return textBody, htmlBody
}

// decodeBody decodes Gmail Base64URL data.
func decodeBody(
	data string,
) string {

	if data == "" {
		return ""
	}

	decoded, err := base64.URLEncoding.DecodeString(
		data,
	)

	if err == nil {
		return string(decoded)
	}

	decoded, err = base64.RawURLEncoding.DecodeString(
		data,
	)

	if err != nil {
		return ""
	}

	return string(decoded)
}

// extractAttachments collects attachment metadata.
func extractAttachments(
	payload *gmail.MessagePart,
) []email.Attachment {

	if payload == nil {
		return nil
	}

	attachments := make(
		[]email.Attachment,
		0,
	)

	collectAttachments(
		payload,
		&attachments,
	)

	if len(attachments) == 0 {
		return nil
	}

	return attachments
}

func collectAttachments(
	payload *gmail.MessagePart,
	attachments *[]email.Attachment,
) {

	if payload == nil {
		return
	}

	if payload.Filename != "" &&
		payload.Body != nil &&
		payload.Body.AttachmentId != "" {

		*attachments = append(
			*attachments,
			email.Attachment{
				ID:       payload.Body.AttachmentId,
				Name:     payload.Filename,
				MimeType: payload.MimeType,
				Size:     int64(payload.Body.Size),
			},
		)
	}

	for _, part := range payload.Parts {

		collectAttachments(
			part,
			attachments,
		)
	}
}

// splitAddresses splits comma-separated address headers.
func splitAddresses(
	value string,
) []string {

	value = strings.TrimSpace(
		value,
	)

	if value == "" {
		return nil
	}

	parts := strings.Split(
		value,
		",",
	)

	result := make(
		[]string,
		0,
		len(parts),
	)

	for _, part := range parts {

		part = strings.TrimSpace(
			part,
		)

		if part != "" {
			result = append(
				result,
				part,
			)
		}
	}

	return result
}

// authorize performs Gmail OAuth using a local callback.
func authorize(
	ctx context.Context,
	config *oauth2.Config,
) (*oauth2.Token, error) {

	listener, err := net.Listen(
		"tcp",
		"127.0.0.1:0",
	)

	if err != nil {
		return nil, fmt.Errorf(
			"start OAuth listener: %w",
			err,
		)
	}

	port := listener.Addr().(*net.TCPAddr).
		Port

	redirectURL := fmt.Sprintf(
		"http://127.0.0.1:%d/",
		port,
	)

	config.RedirectURL = redirectURL

	state := fmt.Sprintf(
		"gmail-%d",
		time.Now().UnixNano(),
	)

	codeChan := make(
		chan string,
		1,
	)

	errorChan := make(
		chan error,
		1,
	)

	mux := http.NewServeMux()

	server := &http.Server{
		Handler: mux,
	}

	mux.HandleFunc(
		"/",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {

			if r.URL.Query().Get("state") != state {

				http.Error(
					w,
					"Invalid OAuth state.",
					http.StatusBadRequest,
				)

				select {
				case errorChan <- fmt.Errorf(
					"invalid OAuth state",
				):
				default:
				}

				return
			}

			if oauthError := r.URL.Query().
				Get("error"); oauthError != "" {

				http.Error(
					w,
					"Google authorization failed.",
					http.StatusBadRequest,
				)

				select {
				case errorChan <- fmt.Errorf(
					"Google OAuth error: %s",
					oauthError,
				):
				default:
				}

				return
			}

			code := r.URL.Query().
				Get("code")

			if code == "" {

				http.Error(
					w,
					"No authorization code received.",
					http.StatusBadRequest,
				)

				select {
				case errorChan <- fmt.Errorf(
					"Google did not return an authorization code",
				):
				default:
				}

				return
			}

			fmt.Fprintln(
				w,
				"Authorization successful. You can close this window.",
			)

			select {
			case codeChan <- code:
			default:
			}
		},
	)

	go func() {

		if err := server.Serve(
			listener,
		); err != nil &&
			err != http.ErrServerClosed {

			select {
			case errorChan <- fmt.Errorf(
				"OAuth server: %w",
				err,
			):
			default:
			}
		}
	}()

	authURL := config.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
	)

	fmt.Println(
		"Open this URL in your browser:",
	)

	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()

	fmt.Println(
		"Waiting for Google authorization...",
	)

	select {

	case code := <-codeChan:

		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)

		defer cancel()

		_ = server.Shutdown(
			shutdownCtx,
		)

		token, err := config.Exchange(
			ctx,
			code,
		)

		if err != nil {
			return nil, fmt.Errorf(
				"exchange Gmail authorization code: %w",
				err,
			)
		}

		return token, nil

	case err := <-errorChan:

		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)

		defer cancel()

		_ = server.Shutdown(
			shutdownCtx,
		)

		return nil, err

	case <-ctx.Done():

		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)

		defer cancel()

		_ = server.Shutdown(
			shutdownCtx,
		)

		return nil, ctx.Err()
	}
}

// loadToken loads the Gmail OAuth token.
func loadToken() (*oauth2.Token, error) {

	file, err := os.Open(
		tokenFile,
	)

	if err != nil {
		return nil, err
	}

	defer file.Close()

	var token oauth2.Token

	if err := json.NewDecoder(
		file,
	).Decode(&token); err != nil {

		return nil, fmt.Errorf(
			"decode Gmail token: %w",
			err,
		)
	}

	return &token, nil
}

// saveToken saves the Gmail OAuth token.
func saveToken(
	token *oauth2.Token,
) error {

	if err := os.MkdirAll(
		"credentials",
		0700,
	); err != nil {

		return fmt.Errorf(
			"create credentials directory: %w",
			err,
		)
	}

	file, err := os.OpenFile(
		tokenFile,
		os.O_RDWR|os.O_CREATE|os.O_TRUNC,
		0600,
	)

	if err != nil {
		return fmt.Errorf(
			"create Gmail token: %w",
			err,
		)
	}

	defer file.Close()

	if err := json.NewEncoder(
		file,
	).Encode(token); err != nil {

		return fmt.Errorf(
			"save Gmail token: %w",
			err,
		)
	}

	return nil
}

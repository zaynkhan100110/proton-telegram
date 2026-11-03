package microsoft

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"proton-telegram/email"

	"golang.org/x/oauth2"
)

const (
	defaultClientID = "a60d62a2-489e-4a93-a988-17ddea3abd2b"

	tokenFile = "credentials/microsoft-token.json"

	authURL  = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"
	tokenURL = "https://login.microsoftonline.com/common/oauth2/v2.0/token"

	graphURL = "https://graph.microsoft.com/v1.0"

	providerName = "microsoft"
)

var scopes = []string{
	"openid",
	"profile",
	"email",
	"offline_access",
	"User.Read",
	"Mail.ReadWrite",
	"Mail.Send",
}

type Provider struct {
	client  *http.Client
	account email.Account
}

// -----------------------------------------------------------------------------
// MICROSOFT GRAPH TYPES
// -----------------------------------------------------------------------------

type graphErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type graphProfile struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
}

type graphRecipient struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

type graphBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type graphFlag struct {
	FlagStatus string `json:"flagStatus"`
}

type graphAttachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	IsInline    bool   `json:"isInline"`
}

type graphMessage struct {
	ID               string   `json:"id"`
	ConversationID   string   `json:"conversationId"`
	Subject          string   `json:"subject"`
	ReceivedDateTime string   `json:"receivedDateTime"`
	SentDateTime     string   `json:"sentDateTime"`
	IsRead           bool     `json:"isRead"`
	IsDraft          bool     `json:"isDraft"`
	Importance       string   `json:"importance"`
	Categories       []string `json:"categories"`

	From          *graphRecipient  `json:"from"`
	Sender        *graphRecipient  `json:"sender"`
	ReplyTo       []graphRecipient `json:"replyTo"`
	ToRecipients  []graphRecipient `json:"toRecipients"`
	CCRecipients  []graphRecipient `json:"ccRecipients"`
	BCCRecipients []graphRecipient `json:"bccRecipients"`

	Body graphBody `json:"body"`
	Flag graphFlag `json:"flag"`

	Attachments []graphAttachment `json:"attachments"`
}

type graphMessageList struct {
	Value []graphMessage `json:"value"`

	NextLink string `json:"@odata.nextLink"`
}

type graphAttachmentList struct {
	Value []graphAttachment `json:"value"`

	NextLink string `json:"@odata.nextLink"`
}

type graphSendRecipient struct {
	EmailAddress struct {
		Address string `json:"address"`
	} `json:"emailAddress"`
}

type graphSendBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type graphSendMessage struct {
	Subject      string               `json:"subject"`
	Body         graphSendBody        `json:"body"`
	ToRecipients []graphSendRecipient `json:"toRecipients"`
}

type graphSendRequest struct {
	Message         graphSendMessage `json:"message"`
	SaveToSentItems bool             `json:"saveToSentItems"`
}

type graphReplyRequest struct {
	Comment string `json:"comment"`
}

// -----------------------------------------------------------------------------
// CONSTRUCTOR / AUTHENTICATION
// -----------------------------------------------------------------------------

// New creates and authenticates the Microsoft Graph provider.
func New(ctx context.Context) (*Provider, error) {

	if ctx == nil {
		ctx = context.Background()
	}

	configuredClientID := strings.TrimSpace(os.Getenv("MICROSOFT_CLIENT_ID"))
	if configuredClientID == "" {
		configuredClientID = defaultClientID
	}
	config := &oauth2.Config{
		ClientID: configuredClientID,

		Endpoint: oauth2.Endpoint{
			AuthURL:  authURL,
			TokenURL: tokenURL,
		},

		Scopes: scopes,
	}

	token, err := loadToken()

	if err != nil {

		fmt.Println()
		fmt.Println("No Microsoft token found.")
		fmt.Println("Starting Microsoft OAuth authorization...")
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

	provider := &Provider{
		client: client,
	}

	profile, err := provider.getProfile(ctx)

	if err != nil {

		/*
			If the stored token has become invalid or was revoked,
			remove it and perform OAuth again once.
		*/
		if isUnauthorizedError(err) {

			fmt.Println()
			fmt.Println(
				"Microsoft OAuth token is no longer valid.",
			)
			fmt.Println(
				"Starting Microsoft OAuth authorization again...",
			)
			fmt.Println()

			_ = os.Remove(tokenFile)

			token, authErr := authorize(
				ctx,
				config,
			)

			if authErr != nil {
				return nil, authErr
			}

			if saveErr := saveToken(token); saveErr != nil {
				return nil, saveErr
			}

			client = config.Client(
				ctx,
				token,
			)

			provider.client = client

			profile, err = provider.getProfile(
				ctx,
			)
		}

		if err != nil {
			return nil, err
		}
	}

	address := strings.TrimSpace(
		profile.Mail,
	)

	if address == "" {
		address = strings.TrimSpace(
			profile.UserPrincipalName,
		)
	}

	if address == "" {
		return nil, fmt.Errorf(
			"Microsoft account email address is empty",
		)
	}

	accountID := profile.ID

	if strings.TrimSpace(accountID) == "" {
		accountID = address
	}

	provider.account = email.Account{
		ID:       "microsoft-" + accountID,
		Provider: providerName,
		Address:  address,
		Name:     profile.DisplayName,
		Enabled:  true,
	}

	if provider.account.Name == "" {
		provider.account.Name = address
	}

	return provider, nil
}

// Account returns information about the connected Microsoft account.
func (p *Provider) Account() email.Account {
	return p.account
}

// -----------------------------------------------------------------------------
// SEARCH
// -----------------------------------------------------------------------------

// SearchMessages searches Microsoft mail and retrieves every selected
// message completely before returning it.
func (p *Provider) SearchMessages(
	ctx context.Context,
	mailbox email.Mailbox,
	query string,
	limit int,
) ([]email.Message, error) {

	if limit <= 0 {
		return []email.Message{}, nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	endpoint, err := p.mailboxEndpoint(
		mailbox,
		query,
	)

	if err != nil {
		return nil, err
	}

	/*
		Graph allows up to 1000 messages per page.
		We still fetch the actual complete message separately,
		just like the Gmail provider does.

		This preserves the project's important behavior:
		a message is fully retrieved before the next message
		is processed.
	*/
	result := make(
		[]email.Message,
		0,
		limit,
	)

	nextURL := endpoint

	for nextURL != "" &&
		len(result) < limit {

		var response graphMessageList

		if err := p.graphRequest(
			ctx,
			http.MethodGet,
			nextURL,
			nil,
			&response,
			"list Microsoft messages",
		); err != nil {
			return nil, err
		}

		for _, item := range response.Value {

			if strings.TrimSpace(item.ID) == "" {
				continue
			}

			message, err := p.GetMessage(
				ctx,
				item.ID,
			)

			if err != nil {
				return nil, err
			}

			result = append(
				result,
				message,
			)

			if len(result) >= limit {
				break
			}
		}

		if len(result) >= limit {
			break
		}

		nextURL = response.NextLink
	}

	return result, nil
}

// GetMessage retrieves one complete Microsoft message.
func (p *Provider) GetMessage(
	ctx context.Context,
	id string,
) (email.Message, error) {

	id = strings.TrimSpace(id)

	if id == "" {
		return email.Message{}, fmt.Errorf(
			"Microsoft message ID is empty",
		)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	messageURL :=
		graphURL +
			"/me/messages/" +
			url.PathEscape(id) +
			"?$select=id,conversationId,subject,receivedDateTime,sentDateTime,isRead,isDraft,importance,categories,from,sender,replyTo,toRecipients,ccRecipients,bccRecipients,body,flag"

	var message graphMessage

	/*
		Request the body as text.

		Microsoft Graph normally returns message bodies as HTML,
		but the API supports the outlook.body-content-type
		Prefer header. Asking for text here means normal users
		do not receive raw HTML through Telegram.
	*/
	if err := p.graphRequestWithHeaders(
		ctx,
		http.MethodGet,
		messageURL,
		nil,
		&message,
		"get Microsoft message",
		map[string]string{
			"Prefer": `outlook.body-content-type="text"`,
		},
	); err != nil {
		return email.Message{}, err
	}

	attachments, err := p.getAttachments(
		ctx,
		id,
	)

	if err != nil {
		return email.Message{}, err
	}

	return convertMessage(
		message,
		attachments,
		p.account.ID,
	), nil
}

// -----------------------------------------------------------------------------
// MESSAGE STATE
// -----------------------------------------------------------------------------

// MarkRead marks a Microsoft message as read.
func (p *Provider) MarkRead(
	ctx context.Context,
	id string,
) error {

	id = strings.TrimSpace(id)

	if id == "" {
		return fmt.Errorf(
			"Microsoft message ID is empty",
		)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	body := map[string]bool{
		"isRead": true,
	}

	endpoint :=
		graphURL +
			"/me/messages/" +
			url.PathEscape(id)

	if err := p.graphRequest(
		ctx,
		http.MethodPatch,
		endpoint,
		body,
		nil,
		"mark Microsoft message as read",
	); err != nil {
		return err
	}

	return nil
}

// Trash moves a Microsoft message to Deleted Items.
//
// This is not permanent deletion.
func (p *Provider) Trash(
	ctx context.Context,
	id string,
) error {

	id = strings.TrimSpace(id)

	if id == "" {
		return fmt.Errorf(
			"Microsoft message ID is empty",
		)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	endpoint :=
		graphURL +
			"/me/messages/" +
			url.PathEscape(id) +
			"/move"

	body := map[string]string{
		"destinationId": "deleteditems",
	}

	if err := p.graphRequest(
		ctx,
		http.MethodPost,
		endpoint,
		body,
		nil,
		"move Microsoft message to Deleted Items",
	); err != nil {
		return err
	}

	return nil
}

// -----------------------------------------------------------------------------
// SEND
// -----------------------------------------------------------------------------

// Send sends a plain-text Microsoft message.
func (p *Provider) Send(
	ctx context.Context,
	to []string,
	subject string,
	body string,
) error {

	if ctx == nil {
		ctx = context.Background()
	}

	recipients, err := normalizeRecipients(
		to,
	)

	if err != nil {
		return err
	}

	subject = strings.TrimSpace(
		subject,
	)

	if subject == "" {
		subject = "(no subject)"
	}

	graphRecipients := make(
		[]graphSendRecipient,
		0,
		len(recipients),
	)

	for _, recipient := range recipients {

		var item graphSendRecipient

		item.EmailAddress.Address =
			recipient

		graphRecipients = append(
			graphRecipients,
			item,
		)
	}

	request := graphSendRequest{
		Message: graphSendMessage{
			Subject: subject,

			Body: graphSendBody{
				ContentType: "Text",
				Content:     body,
			},

			ToRecipients: graphRecipients,
		},

		SaveToSentItems: true,
	}

	endpoint := graphURL + "/me/sendMail"

	if err := p.graphRequest(
		ctx,
		http.MethodPost,
		endpoint,
		request,
		nil,
		"send Microsoft message",
	); err != nil {
		return err
	}

	return nil
}

// -----------------------------------------------------------------------------
// REPLY
// -----------------------------------------------------------------------------

// Reply replies to an existing Microsoft message.
func (p *Provider) Reply(
	ctx context.Context,
	messageID string,
	body string,
) error {

	messageID = strings.TrimSpace(
		messageID,
	)

	if messageID == "" {
		return fmt.Errorf(
			"Microsoft message ID is empty",
		)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	request := graphReplyRequest{
		Comment: body,
	}

	endpoint :=
		graphURL +
			"/me/messages/" +
			url.PathEscape(messageID) +
			"/reply"

	if err := p.graphRequest(
		ctx,
		http.MethodPost,
		endpoint,
		request,
		nil,
		"reply to Microsoft message",
	); err != nil {
		return err
	}

	return nil
}

// -----------------------------------------------------------------------------
// MAILBOX MAPPING
// -----------------------------------------------------------------------------

func (p *Provider) mailboxEndpoint(
	mailbox email.Mailbox,
	query string,
) (string, error) {

	base := graphURL

	switch mailbox {

	case email.MailboxInbox:

		base += "/me/mailFolders/inbox/messages"

	case email.MailboxSent:

		base += "/me/mailFolders/sentitems/messages"

	case email.MailboxDraft:

		base += "/me/mailFolders/drafts/messages"

	case email.MailboxSpam:

		base += "/me/mailFolders/junkemail/messages"

	case email.MailboxTrash:

		base += "/me/mailFolders/deleteditems/messages"

	case email.MailboxScheduled:

		base += "/me/mailFolders/scheduled/messages"

	case email.MailboxAllMail:

		base += "/me/messages"

	case email.MailboxStarred:

		base += "/me/messages"

	case email.MailboxImportant:

		base += "/me/messages"

	case email.MailboxSnoozed:

		return "", fmt.Errorf(
			"Microsoft does not provide a direct equivalent of Gmail Snoozed",
		)

	case email.MailboxPurchases:

		return "", fmt.Errorf(
			"Microsoft does not provide a direct equivalent of Gmail Purchases",
		)

	default:

		return "", fmt.Errorf(
			"unsupported Microsoft mailbox: %s",
			mailbox,
		)
	}

	params := url.Values{}

	params.Set(
		"$select",
		"id",
	)

	params.Set(
		"$top",
		"1000",
	)

	/*
		These special mailboxes are represented by message
		properties instead of separate folders.
	*/
	switch mailbox {

	case email.MailboxStarred:

		params.Set(
			"$filter",
			"flag/flagStatus eq 'flagged'",
		)

	case email.MailboxImportant:

		params.Set(
			"$filter",
			"importance eq 'high'",
		)
	}

	query = strings.TrimSpace(
		query,
	)

	if query != "" {

		/*
			Graph supports free-text $search.

			We intentionally don't try to interpret Gmail-specific
			search syntax here. The provider abstraction passes the
			query to the provider, so Microsoft gets a Microsoft
			Graph search.
		*/
		params.Set(
			"$search",
			`"`+query+`"`,
		)
	}

	/*
		Sort newest first.

		For filtered searches, Microsoft Graph has restrictions
		on combining $filter and $orderby. We avoid mixing them
		for Starred/Important and let Graph use its normal result
		order there.
	*/
	if mailbox != email.MailboxStarred &&
		mailbox != email.MailboxImportant {

		params.Set(
			"$orderby",
			"receivedDateTime desc",
		)
	}

	return base + "?" + params.Encode(), nil
}

// -----------------------------------------------------------------------------
// ATTACHMENTS
// -----------------------------------------------------------------------------

func (p *Provider) getAttachments(
	ctx context.Context,
	messageID string,
) ([]graphAttachment, error) {

	messageID = strings.TrimSpace(
		messageID,
	)

	if messageID == "" {
		return nil, fmt.Errorf(
			"Microsoft message ID is empty",
		)
	}

	endpoint :=
		graphURL +
			"/me/messages/" +
			url.PathEscape(messageID) +
			"/attachments?$select=id,name,contentType,size,isInline&$top=1000"

	attachments := make(
		[]graphAttachment,
		0,
	)

	nextURL := endpoint

	for nextURL != "" {

		var response graphAttachmentList

		if err := p.graphRequest(
			ctx,
			http.MethodGet,
			nextURL,
			nil,
			&response,
			"get Microsoft attachments",
		); err != nil {
			return nil, err
		}

		attachments = append(
			attachments,
			response.Value...,
		)

		nextURL = response.NextLink
	}

	return attachments, nil
}

// -----------------------------------------------------------------------------
// GRAPH HTTP
// -----------------------------------------------------------------------------

func (p *Provider) graphRequest(
	ctx context.Context,
	method string,
	endpoint string,
	body interface{},
	response interface{},
	operation string,
) error {

	return p.graphRequestWithHeaders(
		ctx,
		method,
		endpoint,
		body,
		response,
		operation,
		nil,
	)
}

func (p *Provider) graphRequestWithHeaders(
	ctx context.Context,
	method string,
	endpoint string,
	body interface{},
	response interface{},
	operation string,
	headers map[string]string,
) error {

	if p == nil ||
		p.client == nil {

		return fmt.Errorf(
			"Microsoft Graph client is not initialized",
		)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	var reader io.Reader

	if body != nil {

		data, err := json.Marshal(
			body,
		)

		if err != nil {
			return fmt.Errorf(
				"%s: encode request: %w",
				operation,
				err,
			)
		}

		reader = strings.NewReader(
			string(data),
		)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		method,
		endpoint,
		reader,
	)

	if err != nil {
		return fmt.Errorf(
			"%s: create request: %w",
			operation,
			err,
		)
	}

	if body != nil {
		request.Header.Set(
			"Content-Type",
			"application/json",
		)
	}

	for key, value := range headers {
		request.Header.Set(
			key,
			value,
		)
	}

	responseObject, err := p.client.Do(
		request,
	)

	if err != nil {
		return fmt.Errorf(
			"%s: %w",
			operation,
			err,
		)
	}

	defer responseObject.Body.Close()

	if responseObject.StatusCode < 200 ||
		responseObject.StatusCode >= 300 {

		errorBody, _ := io.ReadAll(
			io.LimitReader(
				responseObject.Body,
				1024*1024,
			),
		)

		var graphError graphErrorResponse

		if json.Unmarshal(
			errorBody,
			&graphError,
		) == nil &&
			graphError.Error.Message != "" {

			return fmt.Errorf(
				"%s: Microsoft Graph returned HTTP %d: %s (%s)",
				operation,
				responseObject.StatusCode,
				graphError.Error.Message,
				graphError.Error.Code,
			)
		}

		message := strings.TrimSpace(
			string(errorBody),
		)

		if message == "" {
			message = responseObject.Status
		}

		return fmt.Errorf(
			"%s: Microsoft Graph returned HTTP %d: %s",
			operation,
			responseObject.StatusCode,
			message,
		)
	}

	if response == nil {
		return nil
	}

	if err := json.NewDecoder(
		responseObject.Body,
	).Decode(response); err != nil {

		return fmt.Errorf(
			"%s: decode response: %w",
			operation,
			err,
		)
	}

	return nil
}

// -----------------------------------------------------------------------------
// PROFILE
// -----------------------------------------------------------------------------

func (p *Provider) getProfile(
	ctx context.Context,
) (graphProfile, error) {

	var profile graphProfile

	err := p.graphRequest(
		ctx,
		http.MethodGet,
		graphURL+"/me?$select=id,displayName,mail,userPrincipalName",
		nil,
		&profile,
		"get Microsoft profile",
	)

	if err != nil {
		return graphProfile{}, err
	}

	return profile, nil
}

// -----------------------------------------------------------------------------
// MESSAGE CONVERSION
// -----------------------------------------------------------------------------

func convertMessage(
	message graphMessage,
	attachments []graphAttachment,
	accountID string,
) email.Message {

	messageDate := time.Time{}

	dateValue := strings.TrimSpace(
		message.ReceivedDateTime,
	)

	if dateValue == "" {
		dateValue = strings.TrimSpace(
			message.SentDateTime,
		)
	}

	if dateValue != "" {

		if parsed, err := time.Parse(
			time.RFC3339,
			dateValue,
		); err == nil {

			messageDate = parsed
		}
	}

	bodyText := ""
	bodyHTML := ""

	switch strings.ToLower(
		strings.TrimSpace(
			message.Body.ContentType,
		),
	) {

	case "text":

		bodyText = message.Body.Content

	case "html":

		bodyHTML = message.Body.Content

	default:

		bodyText = message.Body.Content
	}

	category := strings.Join(
		message.Categories,
		", ",
	)

	starred :=
		strings.EqualFold(
			message.Flag.FlagStatus,
			"flagged",
		)

	important :=
		strings.EqualFold(
			message.Importance,
			"high",
		)

	return email.Message{
		ID:        message.ID,
		AccountID: accountID,
		ThreadID:  message.ConversationID,

		From: recipientString(
			message.From,
		),

		To: recipientStrings(
			message.ToRecipients,
		),

		CC: recipientStrings(
			message.CCRecipients,
		),

		BCC: recipientStrings(
			message.BCCRecipients,
		),

		Subject: message.Subject,
		Date:    messageDate,

		BodyText: bodyText,
		BodyHTML: bodyHTML,

		Read:      message.IsRead,
		Starred:   starred,
		Important: important,
		Category:  category,

		Attachments: convertAttachments(
			attachments,
		),
	}
}

func recipientString(
	recipient *graphRecipient,
) string {

	if recipient == nil {
		return ""
	}

	address := strings.TrimSpace(
		recipient.EmailAddress.Address,
	)

	name := strings.TrimSpace(
		recipient.EmailAddress.Name,
	)

	if name == "" {
		return address
	}

	if address == "" {
		return name
	}

	return name + " <" + address + ">"
}

func recipientStrings(
	recipients []graphRecipient,
) []string {

	if len(recipients) == 0 {
		return nil
	}

	result := make(
		[]string,
		0,
		len(recipients),
	)

	for _, recipient := range recipients {

		value := recipientString(
			&recipient,
		)

		if strings.TrimSpace(value) != "" {
			result = append(
				result,
				value,
			)
		}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func convertAttachments(
	attachments []graphAttachment,
) []email.Attachment {

	if len(attachments) == 0 {
		return nil
	}

	result := make(
		[]email.Attachment,
		0,
		len(attachments),
	)

	for _, attachment := range attachments {

		if strings.TrimSpace(
			attachment.Name,
		) == "" {

			continue
		}

		result = append(
			result,
			email.Attachment{
				ID:       attachment.ID,
				Name:     attachment.Name,
				MimeType: attachment.ContentType,
				Size:     attachment.Size,
			},
		)
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

// -----------------------------------------------------------------------------
// RECIPIENT VALIDATION
// -----------------------------------------------------------------------------

func normalizeRecipients(
	recipients []string,
) ([]string, error) {

	result := make(
		[]string,
		0,
		len(recipients),
	)

	for _, recipient := range recipients {

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

			return nil, fmt.Errorf(
				"invalid recipient: %s",
				recipient,
			)
		}

		result = append(
			result,
			recipient,
		)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf(
			"recipient list is empty",
		)
	}

	return result, nil
}

// -----------------------------------------------------------------------------
// OAUTH
// -----------------------------------------------------------------------------

func authorize(
	ctx context.Context,
	config *oauth2.Config,
) (*oauth2.Token, error) {

	if ctx == nil {
		ctx = context.Background()
	}

	/*
		Use a dynamic localhost port.

		Microsoft explicitly supports localhost loopback redirects
		for native/desktop applications, and the port component
		is ignored when matching localhost redirect URIs.
	*/
	listener, err := net.Listen(
		"tcp",
		"localhost:0",
	)

	if err != nil {
		return nil, fmt.Errorf(
			"start Microsoft OAuth listener: %w",
			err,
		)
	}

	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	redirectURL := fmt.Sprintf(
		"http://localhost:%d/",
		port,
	)

	config.RedirectURL = redirectURL

	state := fmt.Sprintf(
		"microsoft-%d",
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
					"invalid Microsoft OAuth state",
				):
				default:
				}

				return
			}

			if oauthError :=
				r.URL.Query().Get("error"); oauthError != "" {

				description :=
					r.URL.Query().Get(
						"error_description",
					)

				http.Error(
					w,
					"Microsoft authorization failed.",
					http.StatusBadRequest,
				)

				select {
				case errorChan <- fmt.Errorf(
					"Microsoft OAuth error: %s: %s",
					oauthError,
					description,
				):
				default:
				}

				return
			}

			code := r.URL.Query().Get(
				"code",
			)

			if code == "" {

				http.Error(
					w,
					"No authorization code received.",
					http.StatusBadRequest,
				)

				select {
				case errorChan <- fmt.Errorf(
					"Microsoft did not return an authorization code",
				):
				default:
				}

				return
			}

			fmt.Fprintln(
				w,
				"Microsoft authorization successful. You can close this window.",
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
				"Microsoft OAuth server: %w",
				err,
			):
			default:
			}
		}
	}()

	authURL := config.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam(
			"prompt",
			"select_account",
		),
	)

	fmt.Println(
		"Open this URL in your browser:",
	)

	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()

	fmt.Println(
		"Waiting for Microsoft authorization...",
	)

	/*
		Try to open the URL automatically where practical.
		If this fails, the URL has already been printed above.
	*/
	openBrowser(authURL)

	select {

	case code := <-codeChan:

		shutdownCtx, cancel :=
			context.WithTimeout(
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
				"exchange Microsoft authorization code: %w",
				err,
			)
		}

		return token, nil

	case err := <-errorChan:

		shutdownCtx, cancel :=
			context.WithTimeout(
				context.Background(),
				5*time.Second,
			)

		defer cancel()

		_ = server.Shutdown(
			shutdownCtx,
		)

		return nil, err

	case <-ctx.Done():

		shutdownCtx, cancel :=
			context.WithTimeout(
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

func openBrowser(
	target string,
) {

	var command string
	var args []string

	switch runtime.GOOS {

	case "windows":

		command = "rundll32"
		args = []string{
			"url.dll,FileProtocolHandler",
			target,
		}

	case "darwin":

		command = "open"
		args = []string{
			target,
		}

	case "linux":

		command = "xdg-open"
		args = []string{
			target,
		}

	default:

		return
	}

	/*
		Don't wait for the browser process.
	*/
	_, _ = os.StartProcess(
		command,
		append(
			[]string{
				command,
			},
			args...,
		),
		&os.ProcAttr{},
	)
}

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
			"decode Microsoft token: %w",
			err,
		)
	}

	return &token, nil
}

func saveToken(
	token *oauth2.Token,
) error {

	if token == nil {
		return fmt.Errorf(
			"Microsoft OAuth token is nil",
		)
	}

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
			"create Microsoft token: %w",
			err,
		)
	}

	defer file.Close()

	if err := json.NewEncoder(
		file,
	).Encode(token); err != nil {

		return fmt.Errorf(
			"save Microsoft token: %w",
			err,
		)
	}

	return nil
}

func isUnauthorizedError(
	err error,
) bool {

	if err == nil {
		return false
	}

	text := strings.ToLower(
		err.Error(),
	)

	return strings.Contains(
		text,
		"returned http 401",
	)
}

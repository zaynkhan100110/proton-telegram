package email

import (
	"context"
	"fmt"
)

// Service manages all connected email providers.
type Service struct {
	providers []Provider
}

// NewService creates an empty email service.
func NewService() *Service {
	return &Service{
		providers: make([]Provider, 0),
	}
}

// AddProvider registers an email provider.
func (s *Service) AddProvider(
	provider Provider,
) {
	if provider == nil {
		return
	}

	s.providers = append(
		s.providers,
		provider,
	)
}

// Providers returns all registered providers.
func (s *Service) Providers() []Provider {
	return s.providers
}

// Provider returns a provider by account ID.
func (s *Service) Provider(
	accountID string,
) (Provider, error) {

	for _, provider := range s.providers {
		if provider.Account().ID == accountID {
			return provider, nil
		}
	}

	return nil, fmt.Errorf(
		"email account not found: %s",
		accountID,
	)
}

// SearchMessages searches a mailbox for an account.
func (s *Service) SearchMessages(
	ctx context.Context,
	accountID string,
	mailbox Mailbox,
	query string,
	limit int,
) ([]Message, error) {

	if limit <= 0 {
		return []Message{}, nil
	}

	provider, err := s.Provider(
		accountID,
	)

	if err != nil {
		return nil, err
	}

	return provider.SearchMessages(
		ctx,
		mailbox,
		query,
		limit,
	)
}

// GetMessage retrieves one complete message.
func (s *Service) GetMessage(
	ctx context.Context,
	accountID string,
	messageID string,
) (Message, error) {

	provider, err := s.Provider(
		accountID,
	)

	if err != nil {
		return Message{}, err
	}

	return provider.GetMessage(
		ctx,
		messageID,
	)
}

// MarkRead marks a message as read.
func (s *Service) MarkRead(
	ctx context.Context,
	accountID string,
	messageID string,
) error {

	provider, err := s.Provider(
		accountID,
	)

	if err != nil {
		return err
	}

	return provider.MarkRead(
		ctx,
		messageID,
	)
}

// TrashMessage moves a message to Trash.
func (s *Service) TrashMessage(
	ctx context.Context,
	accountID string,
	messageID string,
) error {

	provider, err := s.Provider(
		accountID,
	)

	if err != nil {
		return err
	}

	return provider.Trash(
		ctx,
		messageID,
	)
}

// Send sends a new email using the selected account.
func (s *Service) Send(
	ctx context.Context,
	accountID string,
	to []string,
	subject string,
	body string,
) error {

	provider, err := s.Provider(
		accountID,
	)

	if err != nil {
		return err
	}

	return provider.Send(
		ctx,
		to,
		subject,
		body,
	)
}

// Reply replies using the selected account.
func (s *Service) Reply(
	ctx context.Context,
	accountID string,
	messageID string,
	body string,
) error {

	provider, err := s.Provider(
		accountID,
	)

	if err != nil {
		return err
	}

	return provider.Reply(
		ctx,
		messageID,
		body,
	)
}

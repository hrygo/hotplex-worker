package session

import (
	"context"
	"errors"

	"github.com/hrygo/hotplex/pkg/events"
)

var ErrNotImplemented = errors.New("session: not implemented")

type PostgresMessageStore struct{}

var _ MessageStore = (*PostgresMessageStore)(nil)

func NewPostgresMessageStore(ctx context.Context, connString string) (*PostgresMessageStore, error) {
	return nil, ErrNotImplemented
}

func (s *PostgresMessageStore) Append(ctx context.Context, sessionID string, seq int64, eventType string, payload []byte) error {
	return ErrNotImplemented
}

func (s *PostgresMessageStore) GetBySession(ctx context.Context, sessionID string, fromSeq int64) ([]*EventRecord, error) {
	return nil, ErrNotImplemented
}

func (s *PostgresMessageStore) GetOwner(ctx context.Context, sessionID string) (string, error) {
	return "", ErrNotImplemented
}

func (s *PostgresMessageStore) Query(ctx context.Context, sessionID string, fromSeq int64) ([]*events.Envelope, error) {
	return nil, ErrNotImplemented
}

func (s *PostgresMessageStore) Close() error {
	return ErrNotImplemented
}

func RegisterPostgresMessageStore() {
	// requires pgx driver
}

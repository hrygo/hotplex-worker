package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrAuditChainInvalid is returned when the audit log hash chain is broken.
var ErrAuditChainInvalid = errors.New("session store: audit chain invalid")

// AuditRecord represents a single entry in the session audit trail.
type AuditRecord struct {
	ID           int64
	Timestamp    int64
	Action       string
	ActorID      string
	SessionID    string
	Details      map[string]any
	DetailsStr   string
	PreviousHash string
	CurrentHash  string
}

// AppendAudit atomically inserts a new audit entry with a hash-chained integrity check.
func (s *SQLiteStore) AppendAudit(ctx context.Context, action, actorID, sessionID string, details map[string]any) error {
	detailsStr := ""
	if details != nil {
		detailsJSON, err := json.Marshal(details)
		if err != nil {
			return fmt.Errorf("session store: marshal audit details: %w", err)
		}
		detailsStr = string(detailsJSON)
	}

	// Wrap all three SQL statements in a transaction to prevent hash chain
	// corruption from concurrent calls.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("session store: audit begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var previousHash string
	var lastID int64
	row := tx.QueryRowContext(ctx, queries["store.get_last_audit"])
	if err := row.Scan(&lastID, &previousHash); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("session store: get last audit entry: %w", err)
	}
	if lastID == 0 {
		previousHash = ""
	}

	timestamp := time.Now().UnixMilli()

	res, err := tx.ExecContext(ctx, queries["store.append_audit"],
		timestamp, action, actorID, sessionID, detailsStr, previousHash)
	if err != nil {
		return fmt.Errorf("session store: insert audit: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("session store: get last insert id: %w", err)
	}

	data := fmt.Sprintf("%d%d%s%s%s%s%s", id, timestamp, action, actorID, sessionID, detailsStr, previousHash)
	hash := sha256.Sum256([]byte(data))
	currentHash := hex.EncodeToString(hash[:])

	_, err = tx.ExecContext(ctx, queries["store.update_audit_hash"], currentHash, id)
	if err != nil {
		return fmt.Errorf("session store: update audit hash: %w", err)
	}

	return tx.Commit()
}

// GetAuditTrail retrieves all audit records for a session and verifies the hash chain.
func (s *SQLiteStore) GetAuditTrail(ctx context.Context, sessionID string) ([]*AuditRecord, error) {
	rows, err := s.db.QueryContext(ctx, queries["store.get_audit_trail"], sessionID)
	if err != nil {
		return nil, fmt.Errorf("session store: get audit trail: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []*AuditRecord
	for rows.Next() {
		var r AuditRecord
		var detailsJSON sql.NullString
		var previousHash, currentHash string
		if err := rows.Scan(&r.ID, &r.Timestamp, &r.Action, &r.ActorID, &r.SessionID, &detailsJSON, &previousHash, &currentHash); err != nil {
			continue
		}
		r.PreviousHash = previousHash
		r.CurrentHash = currentHash
		if detailsJSON.Valid && detailsJSON.String != "" {
			r.DetailsStr = detailsJSON.String
			if err := json.Unmarshal([]byte(detailsJSON.String), &r.Details); err != nil {
				r.Details = nil
			}
		}
		records = append(records, &r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session store: audit trail scan: %w", err)
	}

	for i, r := range records {
		data := fmt.Sprintf("%d%d%s%s%s%s%s", r.ID, r.Timestamp, r.Action, r.ActorID, r.SessionID, r.DetailsStr, r.PreviousHash)
		hash := sha256.Sum256([]byte(data))
		expectedHash := hex.EncodeToString(hash[:])
		if r.CurrentHash != expectedHash {
			s.log.Error("session store: audit chain broken", "id", r.ID, "session_id", sessionID)
			return records[:i+1], ErrAuditChainInvalid
		}
	}

	return records, nil
}

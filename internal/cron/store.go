package cron

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ErrJobNotFound is returned when a job is not found in the store.
var ErrJobNotFound = errors.New("cron store: job not found")

// Store defines the persistence interface for cron jobs.
type Store interface {
	Create(ctx context.Context, job *CronJob) error
	Update(ctx context.Context, job *CronJob) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*CronJob, error)
	GetByName(ctx context.Context, name string) (*CronJob, error)
	List(ctx context.Context, enabledOnly bool) ([]*CronJob, error)
	UpdateState(ctx context.Context, id string, state CronJobState) error
	UpsertByName(ctx context.Context, job *CronJob) error
}

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db  *sql.DB
	log *slog.Logger
}

// NewSQLiteStore creates a new cron store backed by the given database.
func NewSQLiteStore(db *sql.DB, log *slog.Logger) *SQLiteStore {
	return &SQLiteStore{db: db, log: log.With("component", "cron_store")}
}

const defaultTimeout = 5 * time.Second

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultTimeout)
}

const jobColumns = `id, name, description, enabled,
	schedule_kind, schedule_data, payload_kind, payload_data,
	work_dir, bot_id, owner_id, platform, platform_key,
	timeout_sec, delete_after_run, silent, max_retries, max_runs, expires_at,
	state, created_at, updated_at`

func (s *SQLiteStore) Create(ctx context.Context, job *CronJob) error {
	if job.ID == "" {
		job.ID = GenerateJobID()
	}

	ctx, cancel := withTimeout(ctx)
	defer cancel()

	schedData, payloadData, platformKeyData, stateData, err := marshalJobColumns(job)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO cron_jobs (`+jobColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Name, job.Description, boolToInt(job.Enabled),
		job.Schedule.Kind, schedData, job.Payload.Kind, payloadData,
		job.WorkDir, job.BotID, job.OwnerID, job.Platform, platformKeyData,
		job.TimeoutSec, boolToInt(job.DeleteAfterRun), boolToInt(job.Silent), job.MaxRetries, job.MaxRuns, job.ExpiresAt,
		stateData, job.CreatedAtMs, job.UpdatedAtMs,
	)
	if err != nil {
		return fmt.Errorf("cron store: create job: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Update(ctx context.Context, job *CronJob) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	schedData, payloadData, platformKeyData, stateData, err := marshalJobColumns(job)
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE cron_jobs SET
			name = ?, description = ?, enabled = ?,
			schedule_kind = ?, schedule_data = ?, payload_kind = ?, payload_data = ?,
			work_dir = ?, bot_id = ?, owner_id = ?, platform = ?, platform_key = ?,
			timeout_sec = ?, delete_after_run = ?, silent = ?, max_retries = ?,
			max_runs = ?, expires_at = ?,
			state = ?, updated_at = ?
		WHERE id = ?`,
		job.Name, job.Description, boolToInt(job.Enabled),
		job.Schedule.Kind, schedData, job.Payload.Kind, payloadData,
		job.WorkDir, job.BotID, job.OwnerID, job.Platform, platformKeyData,
		job.TimeoutSec, boolToInt(job.DeleteAfterRun), boolToInt(job.Silent), job.MaxRetries,
		job.MaxRuns, job.ExpiresAt,
		stateData, job.UpdatedAtMs,
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("cron store: update job: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrJobNotFound, job.ID)
	}
	return nil
}

func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	res, err := s.db.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("cron store: delete job: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	return nil
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (*CronJob, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	row := s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM cron_jobs WHERE id = ?`, id)
	return scanJob(row)
}

func (s *SQLiteStore) GetByName(ctx context.Context, name string) (*CronJob, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	row := s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM cron_jobs WHERE name = ?`, name)
	return scanJob(row)
}

func (s *SQLiteStore) List(ctx context.Context, enabledOnly bool) ([]*CronJob, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	q := `SELECT ` + jobColumns + ` FROM cron_jobs`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY created_at`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("cron store: list jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var jobs []*CronJob
	for rows.Next() {
		job, err := scanJobRow(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *SQLiteStore) UpdateState(ctx context.Context, id string, state CronJobState) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("cron store: marshal state: %w", err)
	}

	now := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx,
		`UPDATE cron_jobs SET state = ?, updated_at = ? WHERE id = ?`,
		string(data), now, id,
	)
	if err != nil {
		return fmt.Errorf("cron store: update state: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	return nil
}

// UpsertByName inserts or updates a job by name (idempotent for YAML import).
// It does not overwrite runtime state if the job already exists.
func (s *SQLiteStore) UpsertByName(ctx context.Context, job *CronJob) error {
	existing, err := s.GetByName(ctx, job.Name)
	if err != nil && !errors.Is(err, ErrJobNotFound) {
		return fmt.Errorf("cron store: upsert lookup: %w", err)
	}

	if existing != nil {
		copyJobDefinition(existing, job)
		return s.Update(ctx, existing)
	}
	return s.Create(ctx, job)
}

func scanJob(row *sql.Row) (*CronJob, error) {
	job, err := scanJobRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrJobNotFound
	}
	return job, err
}

type scanner interface{ Scan(...any) error }

func scanJobRow(s scanner) (*CronJob, error) {
	job := &CronJob{}
	var enabled, deleteAfterRun, silent int
	var schedData, payloadData, platformKeyData, stateData string

	err := s.Scan(
		&job.ID, &job.Name, &job.Description, &enabled,
		&job.Schedule.Kind, &schedData, &job.Payload.Kind, &payloadData,
		&job.WorkDir, &job.BotID, &job.OwnerID, &job.Platform, &platformKeyData,
		&job.TimeoutSec, &deleteAfterRun, &silent, &job.MaxRetries, &job.MaxRuns, &job.ExpiresAt,
		&stateData, &job.CreatedAtMs, &job.UpdatedAtMs,
	)
	if err != nil {
		return nil, fmt.Errorf("cron store: scan job: %w", err)
	}
	if err := decodeJobFields(job, enabled, deleteAfterRun, silent, schedData, payloadData, platformKeyData, stateData); err != nil {
		return nil, err
	}
	return job, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// copyJobDefinition copies editable definition fields from src to dst,
// preserving dst's runtime state (ID, State, CreatedAtMs).
func copyJobDefinition(dst, src *CronJob) {
	dst.Schedule = src.Schedule
	dst.Payload = src.Payload
	dst.Description = src.Description
	dst.WorkDir = src.WorkDir
	dst.BotID = src.BotID
	dst.OwnerID = src.OwnerID
	dst.Platform = src.Platform
	dst.PlatformKey = src.PlatformKey
	dst.TimeoutSec = src.TimeoutSec
	dst.DeleteAfterRun = src.DeleteAfterRun
	dst.Silent = src.Silent
	dst.MaxRetries = src.MaxRetries
	dst.MaxRuns = src.MaxRuns
	dst.ExpiresAt = src.ExpiresAt
	dst.Enabled = src.Enabled
	dst.UpdatedAtMs = time.Now().UnixMilli()
}

func decodeJobFields(job *CronJob, enabled, deleteAfterRun, silent int, schedData, payloadData, platformKeyData, stateData string) error {
	job.Enabled = enabled == 1
	job.DeleteAfterRun = deleteAfterRun == 1
	job.Silent = silent == 1
	if err := json.Unmarshal([]byte(schedData), &job.Schedule); err != nil {
		return fmt.Errorf("cron store: unmarshal schedule: %w", err)
	}
	if err := json.Unmarshal([]byte(payloadData), &job.Payload); err != nil {
		return fmt.Errorf("cron store: unmarshal payload: %w", err)
	}
	if err := json.Unmarshal([]byte(platformKeyData), &job.PlatformKey); err != nil {
		return fmt.Errorf("cron store: unmarshal platform_key: %w", err)
	}
	if err := json.Unmarshal([]byte(stateData), &job.State); err != nil {
		return fmt.Errorf("cron store: unmarshal state: %w", err)
	}
	return nil
}

func marshalJobColumns(job *CronJob) (schedData, payloadData, platformKeyData, stateData string, err error) {
	sd, err := json.Marshal(job.Schedule)
	if err != nil {
		return "", "", "", "", fmt.Errorf("cron store: marshal schedule: %w", err)
	}
	pd, err := json.Marshal(job.Payload)
	if err != nil {
		return "", "", "", "", fmt.Errorf("cron store: marshal payload: %w", err)
	}
	pkd, err := json.Marshal(job.PlatformKey)
	if err != nil {
		return "", "", "", "", fmt.Errorf("cron store: marshal platform_key: %w", err)
	}
	std, err := json.Marshal(job.State)
	if err != nil {
		return "", "", "", "", fmt.Errorf("cron store: marshal state: %w", err)
	}
	return string(sd), string(pd), string(pkd), string(std), nil
}

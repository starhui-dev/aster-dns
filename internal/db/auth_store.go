package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/starhui-dev/aster-dns/internal/auth"
)

type authQuerier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type AuthStore struct {
	pool *pgxpool.Pool
	q    authQuerier
	tx   bool
}

func NewAuthStore(pool *pgxpool.Pool) *AuthStore {
	return &AuthStore{pool: pool, q: pool}
}

func (s *AuthStore) WithinTx(ctx context.Context, operation func(auth.Store) error) error {
	if s.tx || s.pool == nil {
		return errors.New("nested authentication transaction is not supported")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return errors.New("begin authentication transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txStore := &AuthStore{q: tx, tx: true}
	if err = operation(txStore); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return mapAuthStoreError("commit authentication transaction", err)
	}
	return nil
}

func mapAuthStoreError(operation string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrNotFound
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case pgerrcode.UniqueViolation, pgerrcode.SerializationFailure:
			return fmt.Errorf("%w: %s", auth.ErrConflict, operation)
		case pgerrcode.ForeignKeyViolation, pgerrcode.CheckViolation:
			return fmt.Errorf("%w: %s", auth.ErrInvalidInput, operation)
		}
	}
	return fmt.Errorf("%s", operation)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

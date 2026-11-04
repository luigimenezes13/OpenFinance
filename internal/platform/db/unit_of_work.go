package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

type SQLUnitOfWork struct {
	database *sql.DB
}

var _ kernel.UnitOfWork = (*SQLUnitOfWork)(nil)

func NewUnitOfWork(database *sql.DB) *SQLUnitOfWork {
	return &SQLUnitOfWork{database: database}
}

func (u *SQLUnitOfWork) Run(ctx context.Context, work func(ctx context.Context) error) error {
	if _, active := TransactionFrom(ctx); active {
		return work(ctx)
	}

	transaction, err := u.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin unit of work: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	if err := work(withTransaction(ctx, transaction)); err != nil {
		return err
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("db: commit unit of work: %w", err)
	}
	return nil
}

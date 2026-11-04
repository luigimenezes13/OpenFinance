package db

import (
	"context"
	"database/sql"
)

type transactionKey struct{}

func withTransaction(ctx context.Context, transaction *sql.Tx) context.Context {
	return context.WithValue(ctx, transactionKey{}, transaction)
}

func TransactionFrom(ctx context.Context) (*sql.Tx, bool) {
	transaction, active := ctx.Value(transactionKey{}).(*sql.Tx)
	return transaction, active
}

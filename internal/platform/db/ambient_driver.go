package db

import (
	"context"
	"database/sql"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

// AmbientDriver lets long-lived Ent clients of different bounded contexts take
// part in the same Unit of Work.
//
// Each statement goes to the transaction carried by ctx when there is one, and
// to the pool otherwise. Opening a transaction while one is active returns a
// no-op transaction, so Ent's own transactions (client.Tx and the ones it opens
// internally for UpdateOne) join the outer unit instead of committing early.
// An Ent client built directly over a *sql.Tx is not an option: its DB and Tx
// methods assert *sql.DB and panic.
type AmbientDriver struct {
	pool *entsql.Driver
}

var _ dialect.Driver = (*AmbientDriver)(nil)

func NewAmbientDriver(database *sql.DB) *AmbientDriver {
	return &AmbientDriver{pool: entsql.OpenDB(dialect.Postgres, database)}
}

func (d *AmbientDriver) Exec(ctx context.Context, query string, args, result any) error {
	return d.executorFor(ctx).Exec(ctx, query, args, result)
}

func (d *AmbientDriver) Query(ctx context.Context, query string, args, rows any) error {
	return d.executorFor(ctx).Query(ctx, query, args, rows)
}

func (d *AmbientDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	if _, active := TransactionFrom(ctx); active {
		return dialect.NopTx(d), nil
	}
	return d.pool.Tx(ctx)
}

func (d *AmbientDriver) Close() error {
	return d.pool.Close()
}

func (d *AmbientDriver) Dialect() string {
	return dialect.Postgres
}

func (d *AmbientDriver) executorFor(ctx context.Context) dialect.ExecQuerier {
	transaction, active := TransactionFrom(ctx)
	if !active {
		return d.pool
	}
	return entsql.Conn{ExecQuerier: transaction}
}

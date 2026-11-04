//go:build integration

package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/luigimenezes13/financial-manager/internal/platform/db"
)

var errWorkFailed = errors.New("work failed")

type probe struct {
	database *sql.DB
	driver   *db.AmbientDriver
	unit     *db.SQLUnitOfWork
	table    string
}

func newProbe(t *testing.T) probe {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("TEST_DATABASE_URL is empty; run make test-integration")
	}

	ctx := context.Background()
	database, err := db.NewSQLDB(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	table := fmt.Sprintf("unit_of_work_probe_%d", time.Now().UnixNano())
	if _, err := database.ExecContext(ctx, "CREATE TABLE "+table+" (label text NOT NULL)"); err != nil {
		t.Fatalf("create probe table: %v", err)
	}

	t.Cleanup(func() {
		_, _ = database.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
		_ = database.Close()
	})

	return probe{
		database: database,
		driver:   db.NewAmbientDriver(database),
		unit:     db.NewUnitOfWork(database),
		table:    table,
	}
}

func (p probe) insert(ctx context.Context, label string) error {
	return p.driver.Exec(ctx, "INSERT INTO "+p.table+" (label) VALUES ($1)", []any{label}, nil)
}

func (p probe) committedCount(t *testing.T) int {
	t.Helper()

	var count int
	row := p.database.QueryRowContext(context.Background(), "SELECT count(*) FROM "+p.table)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	return count
}

func TestIntegrationUnitOfWork(t *testing.T) {
	t.Run("should commit every write when work succeeds", func(t *testing.T) {
		p := newProbe(t)

		err := p.unit.Run(context.Background(), func(ctx context.Context) error {
			if err := p.insert(ctx, "first"); err != nil {
				return err
			}
			return p.insert(ctx, "second")
		})

		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if got := p.committedCount(t); got != 2 {
			t.Fatalf("committed rows = %d, want 2", got)
		}
	})

	t.Run("should roll back every write when work fails", func(t *testing.T) {
		p := newProbe(t)

		err := p.unit.Run(context.Background(), func(ctx context.Context) error {
			if err := p.insert(ctx, "first"); err != nil {
				return err
			}
			return errWorkFailed
		})

		if !errors.Is(err, errWorkFailed) {
			t.Fatalf("run error = %v, want %v", err, errWorkFailed)
		}
		if got := p.committedCount(t); got != 0 {
			t.Fatalf("committed rows = %d, want 0", got)
		}
	})

	t.Run("should roll back and repanic when work panics", func(t *testing.T) {
		p := newProbe(t)

		defer func() {
			if recover() == nil {
				t.Fatal("panic was swallowed")
			}
			if got := p.committedCount(t); got != 0 {
				t.Fatalf("committed rows = %d, want 0", got)
			}
		}()

		_ = p.unit.Run(context.Background(), func(ctx context.Context) error {
			if err := p.insert(ctx, "first"); err != nil {
				return err
			}
			panic("boom")
		})
	})

	t.Run("should join the outer unit instead of committing a nested one", func(t *testing.T) {
		p := newProbe(t)

		err := p.unit.Run(context.Background(), func(ctx context.Context) error {
			nestedErr := p.unit.Run(ctx, func(ctx context.Context) error {
				return p.insert(ctx, "nested")
			})
			if nestedErr != nil {
				return nestedErr
			}
			return errWorkFailed
		})

		if !errors.Is(err, errWorkFailed) {
			t.Fatalf("run error = %v, want %v", err, errWorkFailed)
		}
		if got := p.committedCount(t); got != 0 {
			t.Fatalf("committed rows = %d, want 0", got)
		}
	})

	t.Run("should hand Ent a no-op transaction inside a unit", func(t *testing.T) {
		p := newProbe(t)

		err := p.unit.Run(context.Background(), func(ctx context.Context) error {
			entTransaction, err := p.driver.Tx(ctx)
			if err != nil {
				return err
			}
			if err := entTransaction.Exec(ctx, "INSERT INTO "+p.table+" (label) VALUES ($1)", []any{"ent"}, nil); err != nil {
				return err
			}
			if err := entTransaction.Commit(); err != nil {
				return err
			}
			return errWorkFailed
		})

		if !errors.Is(err, errWorkFailed) {
			t.Fatalf("run error = %v, want %v", err, errWorkFailed)
		}
		if got := p.committedCount(t); got != 0 {
			t.Fatalf("committed rows = %d, want 0 (Ent commit must not end the unit)", got)
		}
	})

	t.Run("should write through the pool outside a unit", func(t *testing.T) {
		p := newProbe(t)

		if err := p.insert(context.Background(), "autocommit"); err != nil {
			t.Fatalf("insert: %v", err)
		}
		if got := p.committedCount(t); got != 1 {
			t.Fatalf("committed rows = %d, want 1", got)
		}
	})

	t.Run("should expose the active transaction only inside a unit", func(t *testing.T) {
		p := newProbe(t)

		if _, active := db.TransactionFrom(context.Background()); active {
			t.Fatal("transaction reported outside a unit")
		}

		err := p.unit.Run(context.Background(), func(ctx context.Context) error {
			if _, active := db.TransactionFrom(ctx); !active {
				return errors.New("no transaction inside the unit")
			}
			return nil
		})
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	})
}

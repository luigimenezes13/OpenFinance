//go:build integration

// Integration tests for the user repository: a real Postgres with the
// migrations generated for this bounded context applied. Run them with
// `make test-integration`.
package entrepo_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/entrepo/ent"
	"github.com/luigimenezes13/financial-manager/internal/platform/db"
)

var (
	testDB     *sql.DB
	testClient *ent.Client
)

const migrationsDir = "../../../../migrations/identity"

func TestMain(main *testing.M) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		fmt.Println("TEST_DATABASE_URL is empty, use `make test-integration`")
		os.Exit(1)
	}

	ctx := context.Background()
	database, err := db.NewSQLDB(ctx, databaseURL)
	if err != nil {
		fmt.Printf("failed to connect to the test Postgres: %v\n", err)
		os.Exit(1)
	}
	testDB = database
	testClient = entrepo.NewClient(database)

	if err := resetSchema(ctx, database); err != nil {
		fmt.Printf("failed to reset the schema: %v\n", err)
		os.Exit(1)
	}
	if err := applyMigrations(ctx, database); err != nil {
		fmt.Printf("failed to apply migrations: %v\n", err)
		os.Exit(1)
	}

	code := main.Run()
	_ = testClient.Close()
	os.Exit(code)
}

// resetSchema drops ONLY the tables owned by this bounded context so the
// migrations run on a clean base. A broad `DROP SCHEMA public` is not an
// option: the test database is shared with Financial Tracking and
// `go test ./...` runs packages in parallel, so it would wipe the other
// context's tables in the middle of its tests.
func resetSchema(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, `DROP TABLE IF EXISTS users CASCADE`)
	return err
}

// applyMigrations runs this context's versioned .sql files in order and
// tolerates NO error. Swallowing "already exists" errors by matching message
// substrings would mask real migration failures, depend on the server locale,
// and let an old test database skip the ALTER of a newer migration, so tests
// would run against a stale schema. Atlas file names start with a timestamp,
// so lexicographic order is chronological order.
func applyMigrations(ctx context.Context, database *sql.DB) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("reading migrations: %w", err)
	}

	files := sqlFileNames(entries)
	sort.Strings(files)

	for _, file := range files {
		statements, err := os.ReadFile(filepath.Join(migrationsDir, file))
		if err != nil {
			return fmt.Errorf("reading %s: %w", file, err)
		}
		if _, err := database.ExecContext(ctx, string(statements)); err != nil {
			return fmt.Errorf("executing %s: %w", file, err)
		}
	}
	return nil
}

func sqlFileNames(entries []os.DirEntry) []string {
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	return files
}

func truncateUsers(t *testing.T) {
	t.Helper()

	if _, err := testDB.ExecContext(context.Background(), `TRUNCATE users`); err != nil {
		t.Fatalf("failed to truncate tables: %v", err)
	}
}

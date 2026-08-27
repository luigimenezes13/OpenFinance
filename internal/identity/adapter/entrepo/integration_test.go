//go:build integration

// Testes de integração do repositório de usuários: Postgres real, com as
// migrations GERADAS deste bounded context aplicadas.
//
// Como rodar: make test-integration
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

// Só as migrations DESTE bounded context — o Identity não conhece nem
// depende das tabelas do Financial Tracking.
const migrationsDir = "../../../../migrations/identity"

func TestMain(main *testing.M) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		fmt.Println("TEST_DATABASE_URL vazio — use `make test-integration`")
		os.Exit(1)
	}

	ctx := context.Background()
	database, err := db.NewSQLDB(ctx, databaseURL)
	if err != nil {
		fmt.Printf("falha conectando no Postgres de teste: %v\n", err)
		os.Exit(1)
	}
	testDB = database
	testClient = entrepo.NewClient(database)

	if err := applyMigrations(ctx, database); err != nil {
		fmt.Printf("falha aplicando migrations: %v\n", err)
		os.Exit(1)
	}

	code := main.Run()
	_ = testClient.Close()
	os.Exit(code)
}

// applyMigrations cria a tabela deste BC se ela ainda não existir.
//
// Diferente do harness do Financial Tracking, este NÃO derruba o schema: os
// dois BCs compartilham o banco de teste, e um `DROP SCHEMA` aqui apagaria
// as tabelas do outro no meio da execução (o `go test ./...` roda os
// packages em paralelo). Por isso as migrations são idempotentes na prática:
// a tabela é criada uma vez e cada teste limpa as linhas.
func applyMigrations(ctx context.Context, database *sql.DB) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("lendo migrations: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	for _, file := range files {
		statements, err := os.ReadFile(filepath.Join(migrationsDir, file))
		if err != nil {
			return fmt.Errorf("lendo %s: %w", file, err)
		}
		// IF NOT EXISTS não vem na migration gerada (ela assume histórico
		// controlado), então a criação repetida é tolerada aqui.
		if _, err := database.ExecContext(ctx, string(statements)); err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("executando %s: %w", file, err)
		}
	}
	return nil
}

// isAlreadyExists reconhece o erro de objeto duplicado do Postgres (42P07 /
// 42710) — é o sinal de que a migration já havia sido aplicada.
func isAlreadyExists(err error) bool {
	message := err.Error()
	return strings.Contains(message, "already exists")
}

// truncateUsers limpa as linhas antes de cada teste.
func truncateUsers(t *testing.T) {
	t.Helper()

	if _, err := testDB.ExecContext(context.Background(), `TRUNCATE users`); err != nil {
		t.Fatalf("falha truncando users: %v", err)
	}
}

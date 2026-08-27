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

	if err := resetSchema(ctx, database); err != nil {
		fmt.Printf("falha resetando schema: %v\n", err)
		os.Exit(1)
	}
	if err := applyMigrations(ctx, database); err != nil {
		fmt.Printf("falha aplicando migrations: %v\n", err)
		os.Exit(1)
	}

	code := main.Run()
	_ = testClient.Close()
	os.Exit(code)
}

// resetSchema derruba APENAS as tabelas deste bounded context, para as
// migrations serem aplicadas em base limpa.
//
// Derrubar só o que é nosso (em vez de `DROP SCHEMA public`) é obrigatório
// aqui: o banco de teste é compartilhado com o Financial Tracking e o
// `go test ./...` roda os packages em PARALELO, então um drop amplo apagaria
// as tabelas do outro BC no meio dos testes dele.
func resetSchema(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, `DROP TABLE IF EXISTS users CASCADE`)
	return err
}

// applyMigrations executa os .sql versionados deste BC, em ordem.
//
// NENHUM erro é tolerado: se uma migration falha, o teste falha. A versão
// anterior engolia erro de "objeto já existe" por comparação de SUBSTRING na
// mensagem, o que era frágil por dois motivos — mascarava falhas reais de
// migration, e mensagem de erro do Postgres é localizável, então a
// comparação depende do idioma do servidor. A tolerância existia só porque o
// harness não limpava antes; com o resetSchema acima, ela deixa de ser
// necessária.
//
// Efeito colateral bom: migration NOVA passa a ser aplicada em banco de
// teste antigo. Com a tolerância, um banco que já tivesse `users` pularia o
// ALTER de uma migration posterior e os testes rodariam contra um schema
// desatualizado.
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
	// Nome de arquivo do Atlas começa com timestamp, então ordem
	// lexicográfica é ordem cronológica.
	sort.Strings(files)

	for _, file := range files {
		statements, err := os.ReadFile(filepath.Join(migrationsDir, file))
		if err != nil {
			return fmt.Errorf("lendo %s: %w", file, err)
		}
		if _, err := database.ExecContext(ctx, string(statements)); err != nil {
			return fmt.Errorf("executando %s: %w", file, err)
		}
	}
	return nil
}

// truncateUsers limpa as linhas antes de cada teste.
func truncateUsers(t *testing.T) {
	t.Helper()

	if _, err := testDB.ExecContext(context.Background(), `TRUNCATE users`); err != nil {
		t.Fatalf("falha truncando users: %v", err)
	}
}

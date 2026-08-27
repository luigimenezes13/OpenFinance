//go:build integration

// Testes de INTEGRAÇÃO dos repositórios: rodam contra um Postgres real, com
// as migrations GERADAS pelo Ent/Atlas aplicadas. A build tag `integration`
// os mantém fora do `go test ./...` do dia a dia (spec §8).
//
// Como rodar: `make test-integration`.
//
// Estes testes provam o que teste unitário não alcança: se o mapper
// ent↔Snapshot está certo, se o upsert do Ent faz o que se espera de um
// aggregate, e se as CHECK constraints geradas a partir do schema realmente
// existem no banco.
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

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo/ent"
	"github.com/luigimenezes13/financial-manager/internal/platform/db"
)

// Compartilhados pelos testes deste package: a conexão crua (pra
// verificação por SQL e pra provar as constraints) e o client do Ent (o que
// os repositórios usam).
var (
	testDB     *sql.DB
	testClient *ent.Client
)

// Só as migrations DESTE bounded context: os testes daqui não precisam (nem
// devem depender) das tabelas do Identity.
const migrationsDir = "../../../../migrations/financialtracking"

// TestMain prepara o schema UMA vez: derruba tudo e aplica as migrations
// versionadas na ordem. Depois cada teste começa truncando as tabelas.
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

// resetSchema garante base limpa derrubando APENAS as tabelas deste bounded
// context.
//
// A versão anterior fazia `DROP SCHEMA public CASCADE`, o que era um bug
// esperando a hora: o banco de teste é compartilhado e o `go test` roda os
// packages em PARALELO, então este drop apagaria a tabela do Identity no meio
// dos testes dele. Listar as tabelas próprias é mais verboso e é a fronteira
// correta — cada BC limpa o que é seu.
//
// As migrations do Atlas são forward-only (não existe .down.sql), então o
// "desfazer" do ambiente de teste é este drop, seguro AQUI e catastrófico em
// produção; por isso vive num arquivo com build tag de teste.
func resetSchema(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx,
		`DROP TABLE IF EXISTS transactions, category_rules, categories, accounts CASCADE`)
	return err
}

// applyMigrations executa os .sql versionados em ordem. Em produção quem
// aplica é o Atlas (que controla histórico e checa o atlas.sum); aqui o
// objetivo é só ter o schema real sem depender do binário instalado.
func applyMigrations(ctx context.Context, database *sql.DB) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("lendo migrations/: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		// atlas.sum é o arquivo de hashes, não SQL.
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

// truncateAll zera as tabelas antes de cada teste. Estes testes NÃO usam
// t.Parallel(): compartilham um banco só, e paralelizar faria um teste
// truncar as linhas do outro — a isolação aqui é sequencial, de propósito.
func truncateAll(t *testing.T) {
	t.Helper()

	_, err := testDB.ExecContext(context.Background(),
		`TRUNCATE transactions, category_rules, categories, accounts`)
	if err != nil {
		t.Fatalf("falha truncando tabelas: %v", err)
	}
}

//go:build ignore

// Gerador de migrations versionadas do bounded context Identity. Mesmo
// mecanismo do Financial Tracking (Atlas em ModeReplay), com diretório e
// histórico próprios.
//
// Rodar: make migrate-diff-identity name=<nome_da_mudanca>
package main

import (
	"context"
	"database/sql"
	"log"
	"os"

	atlas "ariga.io/atlas/sql/migrate"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/entrepo/ent/migrate"
)

// O Ent usa o scheme da URL como nome do driver database/sql, e o pgx se
// registra como "pgx" — registrar sob "postgres" evita trazer um segundo
// driver só pra gerar migration.
func init() {
	sql.Register("postgres", stdlib.GetDefaultDriver())
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalln("uso: go run ./internal/identity/adapter/entrepo/ent/migrate/main.go <nome_da_migration>")
	}
	name := os.Args[1]

	devURL := os.Getenv("ATLAS_DEV_DATABASE_URL")
	if devURL == "" {
		log.Fatalln("ATLAS_DEV_DATABASE_URL vazio — use `make migrate-diff-identity`")
	}

	directory, err := atlas.NewLocalDir("migrations/identity")
	if err != nil {
		log.Fatalf("falha abrindo migrations/identity: %v", err)
	}

	options := []schema.MigrateOption{
		schema.WithDir(directory),
		schema.WithMigrationMode(schema.ModeReplay),
		schema.WithDialect(dialect.Postgres),
		schema.WithFormatter(atlas.DefaultFormatter),
		schema.WithDropColumn(true),
		schema.WithDropIndex(true),
	}

	if err := migrate.NamedDiff(context.Background(), devURL, name, options...); err != nil {
		log.Fatalf("falha gerando migration: %v", err)
	}
}

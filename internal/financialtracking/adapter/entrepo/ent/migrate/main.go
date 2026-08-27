//go:build ignore

// Gerador de MIGRATIONS VERSIONADAS a partir do schema do Ent — o
// equivalente do `prisma migrate dev`: ele compara o schema declarado
// (ent/schema) com o estado acumulado das migrations já existentes e escreve
// só o SQL da diferença em migrations/.
//
// A build tag `ignore` mantém este arquivo fora do build do package: é um
// programa, não parte da biblioteca.
//
// Rodar: make migrate-diff name=<nome_da_mudanca>
//
// Como funciona por baixo (ModeReplay): o Atlas REAPLICA as migrations
// existentes num banco de desenvolvimento descartável, compara o resultado
// com o schema desejado e gera o delta. Por isso precisa de um banco dev de
// verdade — e por isso ele não pode apontar pro banco de produção.
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

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo/ent/migrate"
)

// O Ent usa o SCHEME da URL como nome do driver database/sql, então
// `postgres://...` faz ele procurar um driver chamado "postgres" — e o pgx
// se registra como "pgx". Registrar o driver do pgx sob o nome "postgres"
// resolve sem trazer um segundo driver (lib/pq) só pra gerar migration.
func init() {
	sql.Register("postgres", stdlib.GetDefaultDriver())
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalln("uso: go run ./ent/migrate <nome_da_migration>")
	}
	name := os.Args[1]

	devURL := os.Getenv("ATLAS_DEV_DATABASE_URL")
	if devURL == "" {
		log.Fatalln("ATLAS_DEV_DATABASE_URL vazio — use `make migrate-diff name=...`")
	}

	// Cada bounded context tem o SEU diretório de migrations, com histórico
	// próprio: é o que permite evoluir o schema de um sem tocar no do outro,
	// e o que impede um BC de referenciar tabela do outro sem perceber.
	//
	// O diretório é a fonte da verdade do histórico: o Atlas mantém um
	// atlas.sum com o hash de cada arquivo, então editar migration já
	// aplicada à mão passa a ser um erro detectável em vez de um drift
	// silencioso.
	directory, err := atlas.NewLocalDir("migrations/financialtracking")
	if err != nil {
		log.Fatalf("falha abrindo migrations/: %v", err)
	}

	options := []schema.MigrateOption{
		schema.WithDir(directory),
		schema.WithMigrationMode(schema.ModeReplay),
		schema.WithDialect(dialect.Postgres),
		schema.WithFormatter(atlas.DefaultFormatter),
		// Coluna e índice removidos do schema saem do banco. Sem isso, o
		// schema declarado e o banco divergem pra sempre em cada remoção.
		schema.WithDropColumn(true),
		schema.WithDropIndex(true),
	}

	if err := migrate.NamedDiff(context.Background(), devURL, name, options...); err != nil {
		log.Fatalf("falha gerando migration: %v", err)
	}
}

// Package entrepo implementa as portas Repository do Financial Tracking BC
// com o client tipado gerado pelo Ent.
//
// Três disciplinas valem pra todo arquivo daqui:
//
//  1. AS ENTITIES DO ENT NÃO SÃO O DOMÍNIO. `ent.Account` é modelo de
//     persistência (campos exportados, zero invariante); o aggregate mora em
//     domain/. A ponte é o Snapshot: ent → Snapshot → FromSnapshot na
//     leitura, Snapshot() → setters na escrita. É o mesmo padrão
//     Prisma-model → domain-mapper do TypeScript.
//
//  2. ERRO DO ENT NÃO VAZA. ent.IsNotFound vira o ErrNotFound do aggregate;
//     o resto sai embrulhado com %w.
//
//  3. NADA DE ENT NO DOMÍNIO NEM NA APPLICATION. A dependência mora só
//     aqui e no composition root — trocar Ent por outra coisa não toca use
//     case nenhum (foi exatamente o que aconteceu quando trocamos pgx cru
//     por Ent: só esta pasta e as migrations mudaram).
package entrepo

import (
	"context"
	"database/sql"
	"fmt"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo/ent"
)

// NewClient monta o client do Ent sobre um *sql.DB já configurado (pool e
// Ping ficam em platform/db, que não conhece bounded context).
//
// Por que receber o *sql.DB em vez de abrir a conexão aqui: o mesmo pool é
// compartilhado por todos os repositórios e pelo health check; abrir por
// adapter multiplicaria conexões sem necessidade.
func NewClient(database *sql.DB) *ent.Client {
	driver := entsql.OpenDB(dialect.Postgres, database)
	return ent.NewClient(ent.Driver(driver))
}

// withTx roda `work` dentro de uma transação do Ent: commit no sucesso,
// rollback em erro OU panic.
//
// Usado onde a escrita abrange mais de uma tabela DENTRO do mesmo aggregate
// (categoria + suas regras). Não existe helper abrangendo dois aggregates:
// isso seria furar a fronteira de consistência.
func withTx(ctx context.Context, client *ent.Client, work func(tx *ent.Tx) error) error {
	transaction, err := client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("entrepo: falha abrindo transação: %w", err)
	}

	// O panic é re-lançado depois do rollback: engolir panic aqui
	// transformaria bug de programação em erro silencioso.
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = transaction.Rollback()
			panic(recovered)
		}
	}()

	if err := work(transaction); err != nil {
		if rollbackErr := transaction.Rollback(); rollbackErr != nil {
			return fmt.Errorf("entrepo: %w (rollback falhou: %v)", err, rollbackErr)
		}
		return err
	}

	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("entrepo: falha no commit: %w", err)
	}
	return nil
}

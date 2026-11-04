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
	"database/sql"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo/ent"
	"github.com/luigimenezes13/financial-manager/internal/platform/db"
)

// NewClient monta o client do Ent sobre um *sql.DB já configurado (pool e
// Ping ficam em platform/db, que não conhece bounded context).
//
// Por que receber o *sql.DB em vez de abrir a conexão aqui: o mesmo pool é
// compartilhado por todos os repositórios e pelo health check; abrir por
// adapter multiplicaria conexões sem necessidade.
func NewClient(database *sql.DB) *ent.Client {
	return ent.NewClient(ent.Driver(db.NewAmbientDriver(database)))
}

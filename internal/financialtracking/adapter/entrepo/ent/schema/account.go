// Package schema descreve o SCHEMA DE PERSISTÊNCIA do Financial Tracking BC
// — é o análogo do prisma.schema, escrito em Go. Daqui saem duas coisas por
// codegen: o client tipado (ent/) e as migrations versionadas (migrations/).
//
// IMPORTANTE, e é o que sustenta a arquitetura: isto NÃO é o modelo de
// domínio. Os aggregates continuam em domain/ com campos unexported e
// invariantes próprias; as entities do Ent são modelo de PERSISTÊNCIA, e o
// mapper do repositório converte ent ↔ Snapshot. Mesmo padrão do
// Prisma-model → domain-mapper que você usa em TypeScript.
//
// Duas disciplinas do schema:
//
//  1. SEM EDGE ENTRE AGGREGATES. account, transaction e category se
//     referenciam por campo UUID puro, sem FK: são fronteiras de
//     consistência separadas. Edge só DENTRO do aggregate
//     (category → rules), onde o ciclo de vida é o mesmo.
//
//  2. CHECK só pra invariante estrutural; vocabulário (kind, currency,
//     assigned_by) fica fora do banco — a lista válida é do domínio.
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Account é a tabela do aggregate Account.
type Account struct {
	ent.Schema
}

// Annotations fixa o nome da tabela (o default do Ent seria "accounts"
// mesmo, mas explícito evita surpresa em rename) e as CHECK constraints.
func (Account) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "accounts"},
		entsql.Checks(map[string]string{
			"accounts_name_not_blank": "btrim(name) <> ''",
			// A invariante do VO Source no banco: conta openfinance SEMPRE
			// tem referência do provider; conta manual NUNCA tem.
			"accounts_source_pair": "(source_provider = '' AND source_provider_account_id = '') " +
				"OR (source_provider <> '' AND source_provider_account_id <> '')",
		}),
	}
}

// Fields espelha 1:1 o account.AccountSnapshot — é o contrato que o mapper
// atravessa.
func (Account) Fields() []ent.Field {
	return []ent.Field{
		// A identidade vem do DOMÍNIO (o aggregate gera o próprio UUID), por
		// isso não há DefaultFunc aqui: o banco não inventa id.
		field.UUID("id", uuid.UUID{}).Immutable(),

		// user_id é referência cruzada de bounded context (Identity): UUID
		// indexado, sem FK — BCs não compartilham tabela.
		field.UUID("user_id", uuid.UUID{}).Immutable(),

		field.String("name"),
		field.String("kind"),

		field.Int64("balance_amount"),
		field.String("balance_currency"),
		field.Time("balance_as_of"),

		field.String("source_provider").Default(""),
		field.String("source_provider_account_id").Default(""),

		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes: busca por dono e a unicidade da conta do provider.
func (Account) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),

		// Índice único PARCIAL: uma conta do provider resolve pra uma conta
		// local. O WHERE é essencial — sem ele, a segunda conta manual do
		// sistema colidiria, porque todas têm ('', '').
		index.Fields("source_provider", "source_provider_account_id").
			Unique().
			Annotations(entsql.IndexWhere("source_provider <> ''")),
	}
}

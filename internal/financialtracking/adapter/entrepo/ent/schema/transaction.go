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

// Transaction é a tabela do aggregate Transaction.
type Transaction struct {
	ent.Schema
}

func (Transaction) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "transactions"},
		entsql.Checks(map[string]string{
			"transactions_amount_not_zero":       "amount <> 0",
			"transactions_description_not_blank": "btrim(description) <> ''",

			// A atribuição de categoria é um VO: os três campos andam
			// juntos ou nenhum vem.
			"transactions_category_assignment_complete": "(category_id IS NULL AND category_assigned_by = '' AND category_assigned_at IS NULL) " +
				"OR (category_id IS NOT NULL AND category_assigned_by <> '' AND category_assigned_at IS NOT NULL)",

			// A invariante do VO ExternalRef.
			"transactions_external_ref_pair": "(external_ref_provider = '' AND external_ref_provider_transaction_id = '') " +
				"OR (external_ref_provider <> '' AND external_ref_provider_transaction_id <> '')",

			// Mesma recusa do MarkReconciled: transação manual não concilia.
			"transactions_reconciled_requires_ref": "reconciled = FALSE OR external_ref_provider <> ''",
		}),
	}
}

func (Transaction) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Immutable(),
		field.UUID("user_id", uuid.UUID{}).Immutable(),

		// Conta e categoria são OUTROS aggregates: campo UUID puro, sem
		// edge e sem FK. Quem garante que a conta existe é o use case (que
		// a carrega e checa o dono), não o banco.
		field.UUID("account_id", uuid.UUID{}).Immutable(),

		field.Int64("amount"),
		field.String("currency"),
		field.Time("occurred_at"),
		field.String("description"),
		field.Bool("reconciled").Default(false),

		// Atribuição de categoria: anulável em bloco.
		field.UUID("category_id", uuid.UUID{}).Optional().Nillable(),
		field.String("category_assigned_by").Default(""),
		field.Time("category_assigned_at").Optional().Nillable(),

		// Referência no provider: provider vazio = transação manual.
		field.String("external_ref_provider").Default(""),
		field.String("external_ref_provider_transaction_id").Default(""),

		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Transaction) Indexes() []ent.Index {
	return []ent.Index{
		// Extrato do usuário: o acesso mais comum é "minhas transações, das
		// recentes pras antigas".
		index.Fields("user_id", "occurred_at"),
		index.Fields("account_id"),
		index.Fields("category_id"),

		// NÃO existe unique em (external_ref_provider,
		// external_ref_provider_transaction_id): o id de transação da Pluggy
		// não é estável (doc §5.5), então não serve de chave de
		// idempotência. Constraint que promete idempotência e quebra no
		// primeiro reprocessamento é pior que a ausência dela — a chave real
		// (fingerprint) é decisão do PR4.
	}
}

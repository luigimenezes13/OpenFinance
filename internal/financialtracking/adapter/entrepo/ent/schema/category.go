package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// Category é a tabela do aggregate Category.
type Category struct {
	ent.Schema
}

func (Category) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "categories"},
		entsql.Checks(map[string]string{
			"categories_name_not_blank":  "btrim(name) <> ''",
			"categories_parent_not_self": "parent_id IS NULL OR parent_id <> id",
		}),
	}
}

func (Category) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Immutable(),
		field.UUID("user_id", uuid.UUID{}).Immutable(),
		field.String("name"),

		// O pai é OUTRA instância do mesmo aggregate — logo campo UUID
		// anulável, sem edge self-referencial: hierarquia de categorias não
		// é composição, cada categoria tem seu próprio ciclo de vida.
		field.UUID("parent_id", uuid.UUID{}).Optional().Nillable(),

		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges: o ÚNICO edge do schema. A regra vive dentro da fronteira do
// aggregate Category, então FK com CASCADE é o que expressa a verdade —
// apagou a categoria, as regras vão com ela.
func (Category) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("rules", CategoryRule.Type).
			Annotations(entsql.Annotation{OnDelete: entsql.Cascade}),
	}
}

func (Category) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("parent_id"),
	}
}

// CategoryRule é a tabela da entity CategoryRule (interna ao aggregate).
type CategoryRule struct {
	ent.Schema
}

func (CategoryRule) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "category_rules"},
		entsql.Checks(map[string]string{
			"category_rules_keyword_not_blank": "btrim(keyword) <> ''",
		}),
	}
}

func (CategoryRule) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Immutable(),

		// Edge field: a coluna da FK é declarada aqui pra ter o nome
		// category_id (o default do Ent seria derivado do nome do edge) e
		// pra o mapper poder lê-la direto, sem carregar o edge.
		field.UUID("category_id", uuid.UUID{}),

		field.String("keyword"),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (CategoryRule) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("category", Category.Type).
			Ref("rules").
			Field("category_id").
			Unique().
			Required(),
	}
}

func (CategoryRule) Indexes() []ent.Index {
	return []ent.Index{
		// Mesma recusa do Category.AddRule: duas regras com o mesmo termo
		// são redundantes.
		index.Fields("category_id", "keyword").Unique(),
	}
}

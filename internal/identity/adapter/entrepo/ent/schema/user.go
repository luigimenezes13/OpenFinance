// Package schema descreve o schema de persistência do bounded context
// Identity. É modelo de PERSISTÊNCIA, não de domínio: o aggregate User vive
// em identity/domain com campos unexported, e a ponte é o Snapshot.
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

// User é a tabela do aggregate User.
type User struct {
	ent.Schema
}

func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "users"},
		entsql.Checks(map[string]string{
			"users_email_not_blank": "btrim(email) <> ''",
			"users_name_not_blank":  "btrim(name) <> ''",
			// A invariante do VO ExternalIdentity: provedor e subject
			// existem juntos, sempre.
			"users_external_identity_complete": "btrim(external_provider) <> '' AND btrim(external_subject) <> ''",
		}),
	}
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		// A identidade vem do domínio; o banco não inventa id.
		field.UUID("id", uuid.UUID{}).Immutable(),

		// Perfil: chega do provedor a cada login e pode mudar.
		field.String("email"),
		field.String("name"),

		// Âncora do usuário. Immutable no schema porque trocar o subject
		// seria trocar de pessoa — e o UpdateNewValues do upsert respeita
		// isso, então nem um bug no mapper consegue reescrever a âncora.
		field.String("external_provider").Immutable(),
		field.String("external_subject").Immutable(),

		field.Time("registered_at").Immutable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		// A consulta do LOGIN, e a garantia de que um subject resolve pra um
		// único usuário local. Sem o unique, uma corrida entre dois primeiros
		// acessos simultâneos criaria dois usuários pro mesmo Google — e o
		// segundo login passaria a devolver um deles ao acaso.
		index.Fields("external_provider", "external_subject").Unique(),

		// E-mail indexado mas NÃO único: identidade é (provedor, subject),
		// não e-mail. Com um segundo provedor no futuro, a mesma pessoa pode
		// aparecer com o mesmo endereço em dois logins distintos, e um
		// unique aqui recusaria o cadastro legítimo.
		index.Fields("email"),
	}
}

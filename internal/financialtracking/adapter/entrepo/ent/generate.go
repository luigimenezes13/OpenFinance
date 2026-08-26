package ent

// A geração do client tipado. Duas features ligadas:
//
//   - sql/upsert: habilita o OnConflict, que permite a porta ter um Save
//     único (insert OU update) sem o repositório consultar antes pra decidir.
//   - sql/versioned-migration: gera o migrate.NamedDiff, usado pelo
//     programa que ESCREVE os arquivos de migration a partir do schema
//     (o equivalente do `prisma migrate dev`).
//
// Rodar: make generate
//go:generate go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/versioned-migration ./schema

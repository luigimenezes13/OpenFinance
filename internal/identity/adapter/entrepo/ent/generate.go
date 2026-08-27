package ent

// Client tipado do bounded context Identity, com histórico de migrations
// próprio. Separado do client do Financial Tracking de propósito: assim o
// compilador garante que este BC não alcança as entities do outro, e cada um
// evolui o schema sem tocar no do vizinho.
//
// Rodar: make generate
//go:generate go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/versioned-migration ./schema

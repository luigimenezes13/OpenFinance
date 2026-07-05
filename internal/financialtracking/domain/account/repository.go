package account

import "context"

// Repository é a PORTA de persistência do aggregate (lê-se account.Repository
// no chamador — por isso sem o prefixo Account, que viraria gagueira).
// Interface no domain, implementação concreta em adapter/pgxrepo — DIP na
// prática: o domínio dita o contrato, a infraestrutura obedece.
//
// Repositories lidam APENAS com o aggregate root, nunca com VOs soltos.
//
// DDD: Port (Repository) — contrato no domain, implementação em adapter.
type Repository interface {
	// Save persiste o aggregate (insert ou update).
	Save(ctx context.Context, account *Account) error

	// FindByID hidrata o aggregate pelo ID. Não encontrado → ErrNotFound
	// (a implementação traduz pgx.ErrNoRows — erro de driver não vaza).
	FindByID(ctx context.Context, id AccountID) (*Account, error)
}

package transaction

import "context"

// Repository é a porta de persistência do aggregate. Interface no domain,
// implementação concreta em adapter/pgxrepo. Lida apenas com o root.
//
// DDD: Port (Repository) — contrato no domain, implementação em adapter.
type Repository interface {
	// Save persiste o aggregate (insert ou update).
	Save(ctx context.Context, transaction *Transaction) error

	// FindByID hidrata o aggregate pelo ID. Não encontrado → ErrNotFound.
	FindByID(ctx context.Context, id TransactionID) (*Transaction, error)
}

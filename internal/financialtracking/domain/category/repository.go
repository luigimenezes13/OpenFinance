package category

import (
	"context"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// Repository é a PORTA de persistência do aggregate (lê-se
// category.Repository no chamador — por isso sem o prefixo Category, que
// viraria gagueira). Interface no domain, implementação concreta em
// adapter/pgxrepo — DIP na prática: o domínio dita o contrato, a
// infraestrutura obedece.
//
// Repositories lidam APENAS com o aggregate root (Category), incluindo suas
// regras internas — nunca com CategoryRule solta.
//
// DDD: Port (Repository) — contrato no domain, implementação em adapter.
type Repository interface {
	// Save persiste o aggregate (insert ou update), incluindo suas regras.
	Save(ctx context.Context, category *Category) error

	// FindByID hidrata o aggregate pelo ID. Não encontrado → ErrNotFound
	// (a implementação traduz pgx.ErrNoRows — erro de driver não vaza).
	FindByID(ctx context.Context, id CategoryID) (*Category, error)

	// ListByUser devolve as categorias do usuário (com suas regras),
	// ordenadas por nome. Sem paginação, pelo mesmo motivo das contas.
	ListByUser(ctx context.Context, userID shared.UserID) ([]*Category, error)
}

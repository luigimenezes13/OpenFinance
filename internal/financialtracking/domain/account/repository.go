package account

import (
	"context"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

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

	// ListByUser devolve as contas do usuário, ordenadas por nome.
	//
	// Sem paginação de propósito: uma pessoa tem contas na casa das unidades,
	// e paginar isso obrigaria toda tela de saldo a lidar com páginas para
	// resolver um problema que não existe.
	ListByUser(ctx context.Context, userID shared.UserID) ([]*Account, error)
}

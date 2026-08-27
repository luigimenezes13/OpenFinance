package identity

import "context"

// Repository é a PORTA de persistência do aggregate User. Interface no
// domain, implementação em adapter — DIP na prática.
//
// DDD: Port (Repository) — contrato no domain, implementação em adapter.
type Repository interface {
	// Save persiste o aggregate (insert ou update).
	Save(ctx context.Context, user *User) error

	// FindByID hidrata o usuário pelo ID. Não encontrado → ErrNotFound.
	FindByID(ctx context.Context, id UserID) (*User, error)

	// FindByExternalIdentity hidrata o usuário pela referência no provedor.
	// É a consulta do LOGIN: o token traz o subject, e é por ele que se
	// descobre quem é a pessoa aqui dentro. Não encontrado → ErrNotFound,
	// que no fluxo de login significa "primeiro acesso".
	FindByExternalIdentity(ctx context.Context, external ExternalIdentity) (*User, error)
}

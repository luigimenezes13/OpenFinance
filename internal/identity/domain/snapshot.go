package identity

import (
	"time"

	"github.com/google/uuid"
)

// UserSnapshot é o ponto de montagem do aggregate para as bordas de
// persistência (padrão Memento, mesma decisão de 2026-07-07 aplicada aos
// aggregates do Financial Tracking). Primitivos só.
type UserSnapshot struct {
	ID               uuid.UUID
	Email            string
	Name             string
	ExternalProvider string
	ExternalSubject  string
	RegisteredAt     time.Time
}

// Snapshot fotografa o estado atual do usuário. Não emite evento.
func (u *User) Snapshot() UserSnapshot {
	return UserSnapshot{
		ID:               u.id.UUID(),
		Email:            u.email.String(),
		Name:             u.name,
		ExternalProvider: u.external.Provider(),
		ExternalSubject:  u.external.Subject(),
		RegisteredAt:     u.registeredAt,
	}
}

// FromSnapshot remonta o usuário revalidando tudo pelos construtores dos
// VOs — banco corrompido não vira aggregate inválido. Não emite Registered:
// rehidratar não é registrar de novo.
func FromSnapshot(snapshot UserSnapshot) (*User, error) {
	id, err := UserIDFromUUID(snapshot.ID)
	if err != nil {
		return nil, err
	}
	email, err := NewEmail(snapshot.Email)
	if err != nil {
		return nil, err
	}
	canonicalName, err := normalizeName(snapshot.Name)
	if err != nil {
		return nil, err
	}
	external, err := NewExternalIdentity(snapshot.ExternalProvider, snapshot.ExternalSubject)
	if err != nil {
		return nil, err
	}
	if snapshot.RegisteredAt.IsZero() {
		return nil, ErrInvalidRegisteredAt
	}

	return &User{
		id:           id,
		email:        email,
		name:         canonicalName,
		external:     external,
		registeredAt: snapshot.RegisteredAt,
	}, nil
}

package identity

import (
	"time"

	"github.com/google/uuid"
)

// UserSnapshot é o ponto de montagem do aggregate para as bordas de
// persistência (padrão Memento, mesma decisão de 2026-07-07 aplicada aos
// aggregates do Financial Tracking). Primitivos só.
type UserSnapshot struct {
	ID    uuid.UUID
	Email string
	Name  string

	// AvatarURL vazio = usuário sem avatar. String em vez de ponteiro
	// porque, diferente de "categoria não atribuída", aqui vazio e ausente
	// são a MESMA coisa — um ponteiro daria dois jeitos de dizer o mesmo.
	AvatarURL string

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
		AvatarURL:        u.avatar.String(),
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
	avatar, err := avatarFromSnapshot(snapshot)
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
		avatar:       avatar,
		external:     external,
		registeredAt: snapshot.RegisteredAt,
	}, nil
}

// avatarFromSnapshot remonta o avatar opcional: vazio vira zero value (sem
// avatar), preenchido é revalidado.
//
// URL inválida no banco é ERRO, não "sem avatar": o caminho de escrita nunca
// produz isso (o VO valida antes), então uma URL torta ali significa que
// alguém escreveu no banco por fora — e engolir em silêncio esconderia
// exatamente a corrupção que se quer descobrir.
func avatarFromSnapshot(snapshot UserSnapshot) (AvatarURL, error) {
	if snapshot.AvatarURL == "" {
		return AvatarURL{}, nil
	}
	return NewAvatarURL(snapshot.AvatarURL)
}

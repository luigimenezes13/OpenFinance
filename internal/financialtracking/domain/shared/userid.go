package shared

import (
	"errors"

	"github.com/google/uuid"
)

// ErrInvalidUserID indica um UUID que não representa identidade de usuário
// alguma.
var ErrInvalidUserID = errors.New("invalid user id")

// userIdentity é o phantom type que marca identidades de usuário.
type userIdentity struct{}

// UserID é o VO de identidade do usuário dono dos aggregates deste BC.
// Alias (=), não defined type, de propósito: preserva os métodos de TypedID.
//
// DDD: Value Object (identidade) — imutável, auto-validado no construtor.
type UserID = TypedID[userIdentity]

// NewUserID constrói o UserID a partir de um uuid.UUID já convertido pela
// borda, traduzindo a invariante pro sentinel deste contexto.
func NewUserID(value uuid.UUID) (UserID, error) {
	userID, err := TypedIDFromUUID[userIdentity](value)
	if err != nil {
		return UserID{}, ErrInvalidUserID
	}
	return userID, nil
}

package shared

import (
	"errors"

	"github.com/google/uuid"
)

// ErrInvalidIdentifier indica um UUID que não identifica nada: o uuid.Nil.
var ErrInvalidIdentifier = errors.New("invalid identifier")

// TypedID é a identidade tipada do BC. O parâmetro T é um phantom type:
// não aparece em campo nenhum — existe só pra o compilador tratar
// TypedID[userIdentity] e TypedID[accountIdentity] como tipos distintos;
// comparar identidades de aggregates diferentes nem compila.
//
// O domínio não parseia representações externas: construtores recebem
// uuid.UUID já convertido pelos mappers das bordas (toDomain/fromDomain).
//
// DDD: Value Object (identidade) — imutável, política de construção única.
type TypedID[T any] struct {
	value uuid.UUID
}

// NewTypedID gera uma identidade nova. Sem erro: uuid.New() não falha.
func NewTypedID[T any]() TypedID[T] {
	return TypedID[T]{value: uuid.New()}
}

// TypedIDFromUUID constrói a identidade a partir de um uuid.UUID vindo das
// bordas. Nil é recusado: não identifica ninguém e tornaria IsZero ambíguo.
func TypedIDFromUUID[T any](value uuid.UUID) (TypedID[T], error) {
	if value == uuid.Nil {
		return TypedID[T]{}, ErrInvalidIdentifier
	}
	return TypedID[T]{value: value}, nil
}

// UUID expõe o valor bruto pro caminho de saída dos mappers (fromDomain).
func (id TypedID[T]) UUID() uuid.UUID {
	return id.value
}

// String retorna a representação canônica do UUID (lowercase, com hífens).
func (id TypedID[T]) String() string {
	return id.value.String()
}

// Equals compara duas identidades do mesmo tipo por valor.
func (id TypedID[T]) Equals(other TypedID[T]) bool {
	return id.value == other.value
}

// IsZero informa se a identidade é o zero value.
func (id TypedID[T]) IsZero() bool {
	return id.value == uuid.Nil
}

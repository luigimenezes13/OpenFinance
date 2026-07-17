// Package kernel é o Shared Kernel (DDD) do projeto: os building blocks de
// domínio compartilhados pelos bounded contexts — identidade tipada
// (TypedID), o acúmulo de eventos do aggregate root (EventRecorder) e, no
// sub-package events, o contrato de eventos de domínio. Depende só da
// stdlib e de google/uuid; nunca importa um bounded context (a seta de
// dependência aponta sempre para cá).
package kernel

import "github.com/google/uuid"

// TypedID é a identidade tipada. O parâmetro T é um phantom type: não
// aparece em campo nenhum — existe só pra o compilador tratar
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

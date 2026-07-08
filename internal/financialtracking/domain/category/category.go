// Package category conterá o aggregate Category (root + entity
// CategoryRule). Por ora só a identidade: Transaction referencia
// CategoryID no CategoryAssignment — referência entre aggregates é por ID.
package category

import (
	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// categoryIdentity é o phantom type que marca identidades de categoria.
type categoryIdentity struct{}

// CategoryID é o VO de identidade da categoria.
//
// DDD: Value Object (identidade) — imutável, auto-validado no construtor.
type CategoryID = shared.TypedID[categoryIdentity]

// NewCategoryID gera uma identidade nova.
func NewCategoryID() CategoryID {
	return shared.NewTypedID[categoryIdentity]()
}

// CategoryIDFromUUID constrói o CategoryID a partir de um uuid.UUID já
// convertido pela borda, traduzindo a invariante pro sentinel do aggregate.
func CategoryIDFromUUID(value uuid.UUID) (CategoryID, error) {
	categoryID, err := shared.TypedIDFromUUID[categoryIdentity](value)
	if err != nil {
		return CategoryID{}, ErrInvalidID
	}
	return categoryID, nil
}

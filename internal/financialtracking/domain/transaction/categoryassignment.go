package transaction

import (
	"time"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
)

// AssignedBy registra quem categorizou: o usuário ou uma regra automática
// (a rules engine do PR2/PR5). Mesmo idioma de enum do account.Kind.
//
// DDD: Value Object (enum) — conjunto fechado, imutável.
type AssignedBy struct {
	value string
}

// Instâncias válidas de AssignedBy.
var (
	AssignedByUser = AssignedBy{value: "user"}
	AssignedByRule = AssignedBy{value: "rule"}
)

// supportedAssignedBy indexa as instâncias pela forma textual sem duplicar
// literais.
var supportedAssignedBy = map[string]AssignedBy{
	AssignedByUser.value: AssignedByUser,
	AssignedByRule.value: AssignedByRule,
}

// NewAssignedBy reconstrói um AssignedBy a partir da forma textual. Valor
// desconhecido → ErrInvalidAssignment.
func NewAssignedBy(raw string) (AssignedBy, error) {
	assignedBy, ok := supportedAssignedBy[raw]
	if !ok {
		return AssignedBy{}, ErrInvalidAssignment
	}
	return assignedBy, nil
}

// String retorna a forma textual (é o que o repository persiste).
func (a AssignedBy) String() string {
	return a.value
}

// IsZero informa se este AssignedBy foi criado fora dos caminhos válidos.
func (a AssignedBy) IsZero() bool {
	return a.value == ""
}

// CategoryAssignment liga a transação a uma categoria, registrando quem
// atribuiu e quando. Referência ao aggregate Category por ID, nunca por
// objeto.
//
// DDD: Value Object — imutável, auto-validado.
type CategoryAssignment struct {
	categoryID category.CategoryID
	by         AssignedBy
	assignedAt time.Time
}

// NewCategoryAssignment valida e constrói a atribuição, datada de agora.
func NewCategoryAssignment(categoryID category.CategoryID, by AssignedBy) (CategoryAssignment, error) {
	if categoryID.IsZero() {
		return CategoryAssignment{}, ErrInvalidAssignment
	}
	if by.IsZero() {
		return CategoryAssignment{}, ErrInvalidAssignment
	}
	return CategoryAssignment{
		categoryID: categoryID,
		by:         by,
		assignedAt: time.Now(),
	}, nil
}

// CategoryID retorna a categoria atribuída.
func (c CategoryAssignment) CategoryID() category.CategoryID {
	return c.categoryID
}

// By retorna quem atribuiu.
func (c CategoryAssignment) By() AssignedBy {
	return c.by
}

// AssignedAt retorna quando a atribuição aconteceu.
func (c CategoryAssignment) AssignedAt() time.Time {
	return c.assignedAt
}

// IsZero informa se este CategoryAssignment é o zero value. Toda atribuição
// válida tem assignedAt — é o discriminador (padrão do Balance).
func (c CategoryAssignment) IsZero() bool {
	return c.assignedAt.IsZero()
}

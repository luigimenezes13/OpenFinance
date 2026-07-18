// Package category contém o aggregate Category do Financial Tracking BC:
// root Category, a entity CategoryRule (auto-categorização futura), os
// sentinels de erro e a porta Repository.
//
// Category NÃO emite eventos de domínio no v1 (nenhum consumidor
// downstream) — por isso, ao contrário de Account/Transaction, o root NÃO
// embeda kernel.EventRecorder. Nem todo aggregate precisa dele.
package category

import (
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

// categoryIdentity é o phantom type que marca identidades de categoria.
type categoryIdentity struct{}

// CategoryID é o VO de identidade da categoria.
//
// DDD: Value Object (identidade) — imutável, auto-validado no construtor.
type CategoryID = kernel.TypedID[categoryIdentity]

// NewCategoryID gera uma identidade nova.
func NewCategoryID() CategoryID {
	return kernel.NewTypedID[categoryIdentity]()
}

// CategoryIDFromUUID constrói o CategoryID a partir de um uuid.UUID já
// convertido pela borda, traduzindo a invariante pro sentinel do aggregate.
func CategoryIDFromUUID(value uuid.UUID) (CategoryID, error) {
	categoryID, err := kernel.TypedIDFromUUID[categoryIdentity](value)
	if err != nil {
		return CategoryID{}, ErrInvalidID
	}
	return categoryID, nil
}

// Category é o aggregate root. Categorias formam uma hierarquia: parentID
// nil = categoria raiz; parentID preenchido = subcategoria. As regras de
// auto-categorização (CategoryRule) vivem DENTRO da fronteira do aggregate
// — só o root as adiciona/remove.
//
// DDD: Aggregate Root — fronteira de consistência; toda mutação passa por
// aqui. Referencia o pai por ID, nunca por objeto.
type Category struct {
	id       CategoryID
	userID   shared.UserID
	name     string
	parentID *CategoryID // nil = categoria raiz
	rules    []CategoryRule
}

// New valida invariantes e cria uma categoria nova (caminho dos use cases).
// parentID nil = categoria raiz; preenchido = subcategoria. rules nasce
// vazio.
func New(userID shared.UserID, name string, parentID *CategoryID) (*Category, error) {
	if userID.IsZero() {
		return nil, shared.ErrInvalidUserID
	}
	canonicalName, err := normalizeName(name)
	if err != nil {
		return nil, err
	}
	parent, err := normalizeParent(parentID)
	if err != nil {
		return nil, err
	}

	return &Category{
		id:       NewCategoryID(),
		userID:   userID,
		name:     canonicalName,
		parentID: parent,
	}, nil
}

// normalizeName é a única casa da regra "o que é um nome de categoria
// válido": valida e devolve a forma canônica. New e Rename usam o retorno.
// (Mesma regra do account.normalizeName — implementado pra você focar nas
// invariantes do aggregate, não no boilerplate.)
func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", ErrInvalidName
	}
	return name, nil
}

// normalizeParent valida e devolve uma CÓPIA do pai: nil continua nil
// (categoria raiz); pai zero value é recusado com ErrInvalidParent. A cópia
// impede o chamador de mutar a hierarquia por fora do root. New e MoveTo
// compartilham esta regra.
func normalizeParent(parentID *CategoryID) (*CategoryID, error) {
	if parentID == nil {
		return nil, nil
	}
	if parentID.IsZero() {
		return nil, ErrInvalidParent
	}
	parent := *parentID
	return &parent, nil
}

// ID retorna a identidade da categoria.
func (c *Category) ID() CategoryID {
	return c.id
}

// UserID retorna o dono da categoria.
func (c *Category) UserID() shared.UserID {
	return c.userID
}

// Name retorna o nome da categoria.
func (c *Category) Name() string {
	return c.name
}

// ParentID retorna o pai, ou nil se for categoria raiz. Devolve CÓPIA do
// valor apontado: expor o ponteiro interno deixaria o chamador mutar a
// hierarquia por fora do root.
func (c *Category) ParentID() *CategoryID {
	if c.parentID == nil {
		return nil
	}
	parent := *c.parentID
	return &parent
}

// Rules retorna CÓPIA das regras — expor o slice interno deixaria o
// chamador mutar as regras por fora do root.
func (c *Category) Rules() []CategoryRule {
	return slices.Clone(c.rules)
}

// Rename troca o nome da categoria (mesma validação do New, via
// normalizeName). Nome inválido mantém o atual. Sem evento (Category não
// emite no v1).
func (c *Category) Rename(newName string) error {
	canonicalName, err := normalizeName(newName)
	if err != nil {
		return err
	}
	c.name = canonicalName
	return nil
}

// MoveTo re-parenteia a categoria (nil = vira raiz). Recusa pai zero value
// e auto-parentesco (categoria não pode ser pai de si mesma).
func (c *Category) MoveTo(newParentID *CategoryID) error {
	parent, err := normalizeParent(newParentID)
	if err != nil {
		return err
	}
	if parent != nil && parent.Equals(c.id) {
		return ErrInvalidParent
	}

	c.parentID = parent
	return nil
}

// AddRule adiciona uma regra de auto-categorização à categoria. Recusa
// regra zero value e keyword duplicada — duas regras com o mesmo termo são
// redundantes.
func (c *Category) AddRule(rule CategoryRule) error {
	if rule.IsZero() {
		return ErrInvalidRule
	}
	duplicate := slices.ContainsFunc(c.rules, func(existing CategoryRule) bool {
		return existing.Keyword() == rule.Keyword()
	})
	if duplicate {
		return ErrDuplicateRule
	}

	c.rules = append(c.rules, rule)
	return nil
}

// RemoveRule remove a regra pelo ID. Regra inexistente → ErrRuleNotFound.
func (c *Category) RemoveRule(ruleID RuleID) error {
	index := slices.IndexFunc(c.rules, func(existing CategoryRule) bool {
		return existing.ID().Equals(ruleID)
	})
	if index == -1 {
		return ErrRuleNotFound
	}

	c.rules = slices.Delete(c.rules, index, index+1)
	return nil
}

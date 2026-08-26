package category

import (
	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// CategorySnapshot é o ponto de montagem do aggregate para as bordas de
// persistência (padrão Memento, decisão de 2026-07-07). Primitivos só.
//
// As REGRAS vêm dentro do snapshot porque estão dentro da fronteira do
// aggregate: o repository grava categoria + regras como uma unidade, e não
// existe "salvar uma CategoryRule" isolado.
type CategorySnapshot struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Name     string
	ParentID *uuid.UUID // nil = categoria raiz
	Rules    []RuleSnapshot
}

// RuleSnapshot é a fotografia de uma CategoryRule (entity interna).
type RuleSnapshot struct {
	ID      uuid.UUID
	Keyword string
}

// Snapshot fotografa o estado atual da categoria e das suas regras.
func (c *Category) Snapshot() CategorySnapshot {
	snapshot := CategorySnapshot{
		ID:     c.id.UUID(),
		UserID: c.userID.UUID(),
		Name:   c.name,
		Rules:  make([]RuleSnapshot, 0, len(c.rules)),
	}

	if c.parentID != nil {
		parent := *c.parentID
		rawParentID := parent.UUID()
		snapshot.ParentID = &rawParentID
	}

	for _, rule := range c.rules {
		ruleID := rule.ID()
		snapshot.Rules = append(snapshot.Rules, RuleSnapshot{
			ID:      ruleID.UUID(),
			Keyword: rule.Keyword(),
		})
	}

	return snapshot
}

// FromSnapshot remonta a categoria e as regras, revalidando tudo pelos
// construtores. Category não emite evento no v1, então nem há o que
// suprimir aqui.
func FromSnapshot(snapshot CategorySnapshot) (*Category, error) {
	id, err := CategoryIDFromUUID(snapshot.ID)
	if err != nil {
		return nil, err
	}
	userID, err := shared.NewUserID(snapshot.UserID)
	if err != nil {
		return nil, err
	}
	canonicalName, err := normalizeName(snapshot.Name)
	if err != nil {
		return nil, err
	}
	parent, err := parentFromSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	if parent != nil && parent.Equals(id) {
		// Mesma recusa do MoveTo: categoria não é pai de si mesma. Estado
		// impossível vindo do banco morre aqui, não vira aggregate torto.
		return nil, ErrInvalidParent
	}

	rebuilt := &Category{
		id:       id,
		userID:   userID,
		name:     canonicalName,
		parentID: parent,
	}

	// As regras entram pelo AddRule do próprio root: assim keyword duplicada
	// ou regra inválida é recusada com o mesmo critério da escrita, em vez
	// de um segundo caminho de montagem que aceita o que o AddRule recusa.
	for _, ruleSnapshot := range snapshot.Rules {
		rule, err := ruleFromSnapshot(ruleSnapshot)
		if err != nil {
			return nil, err
		}
		if err := rebuilt.AddRule(rule); err != nil {
			return nil, err
		}
	}

	return rebuilt, nil
}

// parentFromSnapshot remonta o pai opcional (nil = categoria raiz).
func parentFromSnapshot(snapshot CategorySnapshot) (*CategoryID, error) {
	if snapshot.ParentID == nil {
		return nil, nil
	}
	parent, err := CategoryIDFromUUID(*snapshot.ParentID)
	if err != nil {
		return nil, err
	}
	return &parent, nil
}

// ruleFromSnapshot remonta a entity CategoryRule preservando a IDENTIDADE
// original — NewCategoryRule geraria um RuleID novo, e a regra lida do banco
// deixaria de ser a mesma regra (o RemoveRule do usuário passaria a mirar um
// id que não existe mais).
func ruleFromSnapshot(snapshot RuleSnapshot) (CategoryRule, error) {
	ruleID, err := RuleIDFromUUID(snapshot.ID)
	if err != nil {
		return CategoryRule{}, err
	}
	keyword, err := normalizeKeyword(snapshot.Keyword)
	if err != nil {
		return CategoryRule{}, err
	}
	return CategoryRule{id: ruleID, keyword: keyword}, nil
}

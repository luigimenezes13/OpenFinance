package category

import (
	"strings"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

// ruleIdentity é o phantom type que marca identidades de regra.
type ruleIdentity struct{}

// RuleID é a identidade da CategoryRule. A regra é uma ENTITY dentro da
// fronteira do aggregate Category: tem identidade própria (por isso um ID),
// mas é acessada e mutada só através do root.
//
// DDD: Value Object (identidade de entity interna).
type RuleID = kernel.TypedID[ruleIdentity]

// NewRuleID gera uma identidade nova.
func NewRuleID() RuleID {
	return kernel.NewTypedID[ruleIdentity]()
}

// RuleIDFromUUID constrói o RuleID a partir de um uuid.UUID já convertido
// pela borda, traduzindo a invariante pro sentinel do aggregate.
func RuleIDFromUUID(value uuid.UUID) (RuleID, error) {
	ruleID, err := kernel.TypedIDFromUUID[ruleIdentity](value)
	if err != nil {
		return RuleID{}, ErrInvalidRuleID
	}
	return ruleID, nil
}

// CategoryRule é a regra de auto-categorização: quando o keyword aparece na
// descrição de uma transação, a rules engine (PR2/PR5) atribui esta
// categoria. No v1 a regra só é MODELADA e guardada — o aplicador automático
// vem depois. Mas o comportamento de "casar" já mora aqui (Matches), porque
// é regra de negócio, não do futuro motor.
//
// DDD: Entity (interna ao aggregate Category) — identidade própria (RuleID),
// ciclo de vida controlado pelo root.
type CategoryRule struct {
	id      RuleID
	keyword string
}

// NewCategoryRule valida e cria uma regra nova, com identidade gerada. O
// keyword é guardado na forma canônica (trim); vazio ou só espaços é
// recusado — regra sem termo não casa nada.
func NewCategoryRule(keyword string) (CategoryRule, error) {
	canonicalKeyword, err := normalizeKeyword(keyword)
	if err != nil {
		return CategoryRule{}, err
	}
	return CategoryRule{
		id:      NewRuleID(),
		keyword: canonicalKeyword,
	}, nil
}

// normalizeKeyword é a única casa da regra "o que é um keyword válido":
// valida e devolve a forma canônica. Compartilhada pelo construtor e pela
// rehidratação (ruleFromSnapshot) — dois caminhos de montagem com critérios
// diferentes seriam duas verdades sobre a mesma regra.
func normalizeKeyword(raw string) (string, error) {
	keyword := strings.TrimSpace(raw)
	if keyword == "" {
		return "", ErrInvalidKeyword
	}
	return keyword, nil
}

// ID retorna a identidade da regra.
func (r CategoryRule) ID() RuleID {
	return r.id
}

// Keyword retorna o termo que dispara a regra.
func (r CategoryRule) Keyword() string {
	return r.keyword
}

// Matches informa se a descrição de uma transação casa com esta regra:
// match case-insensitive por substring. Método com INTENÇÃO, não getter
// genérico — a decisão "essa transação cai nessa regra?" é do domínio.
func (r CategoryRule) Matches(description string) bool {
	return strings.Contains(
		strings.ToLower(description),
		strings.ToLower(r.keyword),
	)
}

// IsZero informa se esta regra é o zero value (criada fora do
// NewCategoryRule). Toda regra válida tem identidade — o RuleID é o
// discriminador.
func (r CategoryRule) IsZero() bool {
	return r.id.IsZero()
}

package transaction

import (
	"time"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

// Criteria descreve uma consulta de extrato: de quem, em que janela, com
// quais filtros.
//
// É um VO do DOMÍNIO, não um DTO de borda, e o UserID é obrigatório nele —
// não existe consulta de extrato "de todo mundo". Com o dono embutido no
// critério, o repositório não tem como esquecer o filtro por usuário, que
// seria vazamento de dado entre contas.
//
// DDD: Value Object (Specification) — imutável, auto-validado.
type Criteria struct {
	userID     shared.UserID
	accountID  *account.AccountID
	categoryID *category.CategoryID
	from       time.Time
	to         time.Time
	page       kernel.Page
}

// CriteriaOption é um filtro opcional. Options funcionais em vez de um
// construtor com sete parâmetros: `NewCriteria(userID, page, nil, nil, zero,
// zero)` não diz nada a quem lê, e cada filtro novo mudaria a assinatura de
// todos os chamadores.
type CriteriaOption func(*Criteria)

// ForAccount restringe a consulta a uma conta.
func ForAccount(accountID account.AccountID) CriteriaOption {
	return func(criteria *Criteria) {
		criteria.accountID = &accountID
	}
}

// ForCategory restringe a consulta a uma categoria.
func ForCategory(categoryID category.CategoryID) CriteriaOption {
	return func(criteria *Criteria) {
		criteria.categoryID = &categoryID
	}
}

// InPeriod restringe a consulta a um intervalo de tempo (inclusive nas duas
// pontas). Instante zero em qualquer ponta significa "sem limite desse
// lado".
func InPeriod(from time.Time, to time.Time) CriteriaOption {
	return func(criteria *Criteria) {
		criteria.from = from
		criteria.to = to
	}
}

// NewCriteria valida e constrói o critério. As options são aplicadas antes
// da validação, então filtro inválido é recusado aqui e não no repositório.
func NewCriteria(userID shared.UserID, page kernel.Page, options ...CriteriaOption) (Criteria, error) {
	if userID.IsZero() {
		return Criteria{}, shared.ErrInvalidUserID
	}
	if page.IsZero() {
		return Criteria{}, kernel.ErrInvalidPage
	}

	criteria := Criteria{userID: userID, page: page}
	for _, option := range options {
		option(&criteria)
	}

	if criteria.accountID != nil && criteria.accountID.IsZero() {
		return Criteria{}, account.ErrInvalidID
	}
	if criteria.categoryID != nil && criteria.categoryID.IsZero() {
		return Criteria{}, category.ErrInvalidID
	}
	// Intervalo invertido é erro, não conjunto vazio: devolver zero
	// resultados para "de dezembro até janeiro" faria o usuário concluir que
	// não gastou nada no período.
	if !criteria.from.IsZero() && !criteria.to.IsZero() && criteria.to.Before(criteria.from) {
		return Criteria{}, ErrInvalidPeriod
	}

	return criteria, nil
}

// UserID retorna o dono do extrato.
func (c Criteria) UserID() shared.UserID {
	return c.userID
}

// AccountID retorna o filtro de conta, com ok=false se não há.
func (c Criteria) AccountID() (account.AccountID, bool) {
	if c.accountID == nil {
		return account.AccountID{}, false
	}
	return *c.accountID, true
}

// CategoryID retorna o filtro de categoria, com ok=false se não há.
func (c Criteria) CategoryID() (category.CategoryID, bool) {
	if c.categoryID == nil {
		return category.CategoryID{}, false
	}
	return *c.categoryID, true
}

// From retorna o início do período, com ok=false se não há.
func (c Criteria) From() (time.Time, bool) {
	return c.from, !c.from.IsZero()
}

// To retorna o fim do período, com ok=false se não há.
func (c Criteria) To() (time.Time, bool) {
	return c.to, !c.to.IsZero()
}

// Page retorna a janela de paginação.
func (c Criteria) Page() kernel.Page {
	return c.page
}

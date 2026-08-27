// Package httperror traduz erro de DOMÍNIO em status HTTP.
//
// Esta é a única casa dessa tradução — os sentinels sobem crus dos
// aggregates, atravessam os use cases sem embrulho e morrem aqui. Se cada
// handler decidisse o próprio status, o mesmo ErrForbidden viraria 403 num
// endpoint e 404 noutro.
//
// O domínio não sabe o que é HTTP; este package sabe os dois lados, e é o
// preço de ter a fronteira num lugar só.
package httperror

import (
	"errors"
	"net/http"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/openfinance"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

// Body é o corpo de erro devolvido pela API. Code é estável e legível por
// máquina (o cliente decide o que fazer por ele); Message é pra humano.
type Body struct {
	Error Detail `json:"error"`
}

// Detail carrega o par código/mensagem.
type Detail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// translation associa um sentinel a um status e um código estável.
type translation struct {
	sentinel error
	status   int
	code     string
}

// translations é uma LISTA, não um switch. Sentinel novo entra sem tocar na
// função de tradução (OCP na prática): o `for` percorre e o errors.Is casa,
// inclusive quando o erro sobe embrulhado com %w.
//
// A ordem importa só entre sentinels que se sobrepõem — e nenhum aqui se
// sobrepõe, porque cada um é uma variável única.
var translations = []translation{
	// 401 — quem chama não provou quem é. As duas variantes existem porque
	// a ação do cliente é diferente: token inválido não tem o que fazer
	// além de logar de novo; token expirado é só renovar. Nenhuma das duas
	// detalha o motivo além disso — dizer "assinatura inválida" vs
	// "audience errada" ajudaria quem está tentando forjar token.
	{identity.ErrInvalidToken, http.StatusUnauthorized, "invalid_token"},
	{identity.ErrTokenExpired, http.StatusUnauthorized, "token_expired"},

	// 403 — ownership e autenticado-mas-não-autorizado. Vem antes por
	// clareza: é a regra de autorização.
	{shared.ErrForbidden, http.StatusForbidden, "forbidden"},
	{identity.ErrEmailNotVerified, http.StatusForbidden, "email_not_verified"},

	// 404 — aggregate inexistente.
	{account.ErrNotFound, http.StatusNotFound, "account_not_found"},
	{transaction.ErrNotFound, http.StatusNotFound, "transaction_not_found"},
	{category.ErrNotFound, http.StatusNotFound, "category_not_found"},
	{category.ErrRuleNotFound, http.StatusNotFound, "category_rule_not_found"},

	// 409 — conflito de estado.
	{category.ErrDuplicateRule, http.StatusConflict, "duplicate_category_rule"},
	{openfinance.ErrConsentExpired, http.StatusConflict, "consent_expired"},

	// 502 — a falha é do provider, não do cliente. 5xx porque tentar de
	// novo mais tarde é a ação certa.
	{openfinance.ErrProviderUnavailable, http.StatusBadGateway, "provider_unavailable"},

	// 422 — a requisição está bem formada, mas viola invariante do domínio.
	// Distinguir de 400 (que os handlers usam pra JSON malformado) diz ao
	// cliente se o problema é a SINTAXE ou a REGRA.
	{shared.ErrInvalidUserID, http.StatusUnprocessableEntity, "invalid_user_id"},
	{shared.ErrInvalidCurrency, http.StatusUnprocessableEntity, "invalid_currency"},
	{shared.ErrCurrencyMismatch, http.StatusUnprocessableEntity, "currency_mismatch"},

	{account.ErrInvalidID, http.StatusUnprocessableEntity, "invalid_account_id"},
	{account.ErrInvalidName, http.StatusUnprocessableEntity, "invalid_account_name"},
	{account.ErrInvalidKind, http.StatusUnprocessableEntity, "invalid_account_kind"},
	{account.ErrInvalidSource, http.StatusUnprocessableEntity, "invalid_account_source"},
	{account.ErrInvalidBalance, http.StatusUnprocessableEntity, "invalid_balance"},
	{account.ErrCurrencyMismatch, http.StatusUnprocessableEntity, "currency_mismatch"},
	{account.ErrStaleBalance, http.StatusConflict, "stale_balance"},

	{transaction.ErrInvalidID, http.StatusUnprocessableEntity, "invalid_transaction_id"},
	{transaction.ErrInvalidMoney, http.StatusUnprocessableEntity, "invalid_money"},
	{transaction.ErrZeroMoney, http.StatusUnprocessableEntity, "zero_amount"},
	{transaction.ErrInvalidOccurredAt, http.StatusUnprocessableEntity, "invalid_occurred_at"},
	{transaction.ErrInvalidDescription, http.StatusUnprocessableEntity, "invalid_description"},
	{transaction.ErrInvalidRef, http.StatusUnprocessableEntity, "invalid_external_ref"},
	{transaction.ErrInvalidAssignment, http.StatusUnprocessableEntity, "invalid_category_assignment"},
	{transaction.ErrNotReconcilable, http.StatusUnprocessableEntity, "not_reconcilable"},

	{category.ErrInvalidID, http.StatusUnprocessableEntity, "invalid_category_id"},
	{category.ErrInvalidRuleID, http.StatusUnprocessableEntity, "invalid_category_rule_id"},
	{category.ErrInvalidName, http.StatusUnprocessableEntity, "invalid_category_name"},
	{category.ErrInvalidParent, http.StatusUnprocessableEntity, "invalid_category_parent"},
	{category.ErrInvalidRule, http.StatusUnprocessableEntity, "invalid_category_rule"},
	{category.ErrInvalidKeyword, http.StatusUnprocessableEntity, "invalid_category_keyword"},

	{openfinance.ErrAccountNotConnected, http.StatusUnprocessableEntity, "account_not_connected"},
	{openfinance.ErrProviderMismatch, http.StatusUnprocessableEntity, "provider_mismatch"},

	// Identity: dado incoerente vindo do provedor de identidade. 422 e não
	// 401, porque o token era válido — o que não presta é o perfil que veio
	// dentro dele.
	{identity.ErrNotFound, http.StatusNotFound, "user_not_found"},
	{identity.ErrInvalidID, http.StatusUnprocessableEntity, "invalid_user_id"},
	{identity.ErrInvalidEmail, http.StatusUnprocessableEntity, "invalid_email"},
	{identity.ErrInvalidName, http.StatusUnprocessableEntity, "invalid_name"},
	{identity.ErrInvalidExternalIdentity, http.StatusUnprocessableEntity, "invalid_external_identity"},
	{identity.ErrInvalidRegisteredAt, http.StatusUnprocessableEntity, "invalid_registered_at"},
}

// Translate devolve o status e o corpo de erro para um erro de domínio.
//
// Erro DESCONHECIDO vira 500 com mensagem genérica: a mensagem original
// pode carregar detalhe de infraestrutura (nome de coluna, host do banco),
// e isso não vai pro cliente. Quem precisa do detalhe é o log, não o
// consumidor da API.
func Translate(err error) (int, Body) {
	for _, candidate := range translations {
		if errors.Is(err, candidate.sentinel) {
			return candidate.status, Body{Error: Detail{
				Code:    candidate.code,
				Message: err.Error(),
			}}
		}
	}

	return http.StatusInternalServerError, Body{Error: Detail{
		Code:    "internal_error",
		Message: "erro interno",
	}}
}

// BadRequest monta o corpo de 400 para falha de SINTAXE (JSON inválido,
// campo obrigatório ausente, uuid mal formado). Não passa pela tabela: aqui
// o erro não é de domínio, é da borda.
func BadRequest(message string) Body {
	return Body{Error: Detail{Code: "bad_request", Message: message}}
}

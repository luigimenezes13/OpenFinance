package account

import "strings"

// Source registra a origem da conta e, quando importada, a referência que
// resolve "conta do provider → nossa conta" no fluxo de import. A
// invariante é protegida na construção: conta openfinance SEMPRE carrega a
// referência do provider; conta manual NUNCA carrega.
//
// DDD: Value Object — imutável, auto-validado, dois caminhos de construção.
type Source struct {
	origin            string
	provider          string
	providerAccountID string
}

// Valores internos de origin. Não-exportados: a origem se escolhe pelo
// construtor, não por constante solta.
const (
	originManual      = "manual"
	originOpenFinance = "openfinance"
)

// NewManualSource cria a origem de uma conta cadastrada à mão pelo usuário.
// Sem erro: não existe manual inválido.
func NewManualSource() Source {
	return Source{origin: originManual}
}

// NewOpenFinanceSource cria a origem de uma conta importada de um provider.
// O formato do providerAccountID não é validado de propósito: cada provider
// tem o seu, e o domínio só exige que exista.
func NewOpenFinanceSource(provider string, providerAccountID string) (Source, error) {
	if strings.TrimSpace(provider) == "" {
		return Source{}, ErrInvalidSource
	}
	if strings.TrimSpace(providerAccountID) == "" {
		return Source{}, ErrInvalidSource
	}
	return Source{
		origin:            originOpenFinance,
		provider:          provider,
		providerAccountID: providerAccountID,
	}, nil
}

// IsOpenFinance informa se a conta veio de um provider Open Finance.
func (s Source) IsOpenFinance() bool {
	return s.origin == originOpenFinance
}

// Provider retorna o nome do provider e ok=true, ou ("", false) se a conta
// é manual.
func (s Source) Provider() (string, bool) {
	if !s.IsOpenFinance() {
		return "", false
	}
	return s.provider, true
}

// ProviderAccountID retorna o id da conta no provider e ok=true, ou
// ("", false) se a conta é manual.
func (s Source) ProviderAccountID() (string, bool) {
	if !s.IsOpenFinance() {
		return "", false
	}
	return s.providerAccountID, true
}

// IsZero informa se este Source foi criado fora dos construtores.
func (s Source) IsZero() bool {
	return s.origin == ""
}

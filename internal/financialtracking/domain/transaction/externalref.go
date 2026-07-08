package transaction

import "strings"

// ExternalRef identifica a transação na origem: o provider e o id dela lá.
//
// ATENÇÃO (doc Pluggy §5.5): o id de transação da Pluggy NÃO é estável —
// muda se date/description/amount mudarem materialmente. A chave de
// idempotência (providerCode ou fingerprint) é decisão do ACL/repository
// no PR4; o domínio só guarda a referência.
//
// DDD: Value Object — imutável, auto-validado.
type ExternalRef struct {
	provider              string
	providerTransactionID string
}

// NewExternalRef valida e constrói a referência. Os dois campos são
// obrigatórios; o formato não se valida (regra de ACL: cada provider tem
// o seu).
func NewExternalRef(provider string, providerTransactionID string) (ExternalRef, error) {
	if strings.TrimSpace(provider) == "" {
		return ExternalRef{}, ErrInvalidRef
	}
	if strings.TrimSpace(providerTransactionID) == "" {
		return ExternalRef{}, ErrInvalidRef
	}
	return ExternalRef{
		provider:              provider,
		providerTransactionID: providerTransactionID,
	}, nil
}

// Provider retorna o nome do provider. Sem comma-ok: num ExternalRef
// válido os campos sempre existem (contraste com Source, onde manual
// não tem provider).
func (r ExternalRef) Provider() string {
	return r.provider
}

// ProviderTransactionID retorna o id da transação no provider.
func (r ExternalRef) ProviderTransactionID() string {
	return r.providerTransactionID
}

// IsZero informa se este ExternalRef foi criado fora do construtor.
func (r ExternalRef) IsZero() bool {
	return r.provider == ""
}

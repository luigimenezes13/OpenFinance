package identity

import "strings"

// ExternalIdentity é a referência do usuário NO PROVEDOR de identidade: o
// nome do provedor e o subject dele lá (o claim `sub` do Google).
//
// É o mesmo padrão do account.Source e do transaction.ExternalRef, terceiro
// uso no projeto: o dado externo entra como VO com nome próprio, em vez de
// string solta espalhada pelo código.
//
// A razão de existir é o que ela EVITA. Sem ela, o caminho natural seria
// usar o `sub` do Google como identidade do usuário no sistema todo — e aí
// cada tabela de cada bounded context ficaria chaveada por uma string de um
// fornecedor. No dia que entrar "entrar com Apple", ou e-mail e senha, ou o
// Google mudar o formato do subject, seria migration em toda tabela. Com a
// referência contida aqui, o resto do sistema só conhece o UserID local.
//
// DDD: Value Object — imutável, sem identidade, auto-validado.
type ExternalIdentity struct {
	provider string
	subject  string
}

// Provedores suportados. Conjunto fechado: o domínio sabe com quem fala.
const (
	ProviderGoogle = "google"
)

// supportedProviders indexa os provedores válidos sem duplicar literais.
var supportedProviders = map[string]struct{}{
	ProviderGoogle: {},
}

// NewExternalIdentity valida e constrói a referência. Provedor desconhecido
// é recusado — aceitar qualquer string deixaria um bug de digitação criar um
// usuário fantasma que nunca mais é encontrado na busca por identidade.
//
// O FORMATO do subject não se valida: cada provedor tem o seu, e o domínio
// só exige que exista.
func NewExternalIdentity(provider string, subject string) (ExternalIdentity, error) {
	canonicalProvider := strings.ToLower(strings.TrimSpace(provider))
	if _, ok := supportedProviders[canonicalProvider]; !ok {
		return ExternalIdentity{}, ErrInvalidExternalIdentity
	}
	if strings.TrimSpace(subject) == "" {
		return ExternalIdentity{}, ErrInvalidExternalIdentity
	}
	return ExternalIdentity{provider: canonicalProvider, subject: subject}, nil
}

// Provider retorna o provedor de identidade.
func (i ExternalIdentity) Provider() string {
	return i.provider
}

// Subject retorna o identificador do usuário no provedor.
func (i ExternalIdentity) Subject() string {
	return i.subject
}

// Equals compara referências por valor.
func (i ExternalIdentity) Equals(other ExternalIdentity) bool {
	return i.provider == other.provider && i.subject == other.subject
}

// IsZero informa se esta referência foi criada fora do construtor.
func (i ExternalIdentity) IsZero() bool {
	return i.provider == ""
}

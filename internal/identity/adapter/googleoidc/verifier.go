// Package googleoidc implementa a porta identity.TokenVerifier contra o
// Google, validando ID tokens (OIDC).
//
// Toda especificidade do Google mora aqui: busca e rotação das chaves
// públicas (JWKS), formato dos claims, mensagens de erro da biblioteca. O
// domínio só conhece VerifiedIdentity e dois sentinels — trocar por Apple ou
// por um IdP corporativo é escrever outro adapter, sem tocar use case.
package googleoidc

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/api/idtoken"

	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

// validateFunc é a assinatura da validação de token. Existe como campo pra
// os testes deste package poderem exercitar o MAPEAMENTO de claims e a
// TRADUÇÃO de erro sem rede — o que a biblioteca faz (verificar assinatura
// com a chave pública do Google) não é o que precisamos testar; o que
// fazemos com o resultado dela, sim.
type validateFunc func(ctx context.Context, rawToken string, audience string) (*idtoken.Payload, error)

// Verifier valida ID tokens do Google.
type Verifier struct {
	audience string
	validate validateFunc
}

// Garante em tempo de compilação que o adapter satisfaz a porta.
var _ identity.TokenVerifier = (*Verifier)(nil)

// NewVerifier cria o verificador para uma audience — o Client ID da
// aplicação no Google.
//
// A audience é OBRIGATÓRIA e é o ponto mais importante deste arquivo. Sem
// validá-la, o serviço aceitaria um token legítimo do Google emitido para
// OUTRO aplicativo: qualquer pessoa com um app Google registrado
// autenticaria como qualquer usuário nosso. É uma das falhas clássicas de
// integração OIDC, e é por isso que aqui ela é erro de inicialização — o
// processo não sobe sem audience, em vez de subir aceitando tudo.
func NewVerifier(audience string) (*Verifier, error) {
	if strings.TrimSpace(audience) == "" {
		return nil, fmt.Errorf("googleoidc: audience (Google Client ID) é obrigatória")
	}

	return &Verifier{
		audience: audience,
		validate: func(ctx context.Context, rawToken string, audience string) (*idtoken.Payload, error) {
			// idtoken.Validate verifica assinatura, issuer, audience e
			// expiração, e mantém as chaves públicas em cache com rotação.
			return idtoken.Validate(ctx, rawToken, audience)
		},
	}, nil
}

// Verify valida o token e devolve o que o Google afirma sobre o portador.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (identity.VerifiedIdentity, error) {
	if strings.TrimSpace(rawToken) == "" {
		return identity.VerifiedIdentity{}, identity.ErrInvalidToken
	}

	payload, err := v.validate(ctx, rawToken, v.audience)
	if err != nil {
		return identity.VerifiedIdentity{}, translateError(err)
	}

	return toVerifiedIdentity(payload), nil
}

// toVerifiedIdentity traduz o payload do Google no contrato do domínio.
//
// Os claims vêm num map[string]any, então cada leitura é uma asserção de
// tipo que pode falhar — e falha SILENCIOSA aqui seria grave: um
// email_verified que não é bool viraria `false`... o que é justamente o
// comportamento seguro (o use case recusa). Ausência de claim nunca vira
// "verificado".
func toVerifiedIdentity(payload *idtoken.Payload) identity.VerifiedIdentity {
	return identity.VerifiedIdentity{
		Provider:      identity.ProviderGoogle,
		Subject:       payload.Subject,
		Email:         stringClaim(payload, "email"),
		Name:          stringClaim(payload, "name"),
		EmailVerified: boolClaim(payload, "email_verified"),
	}
}

// stringClaim lê um claim de texto, devolvendo vazio se ausente ou de outro
// tipo. Quem decide se vazio é problema é o domínio (NewEmail recusa).
func stringClaim(payload *idtoken.Payload, name string) string {
	value, ok := payload.Claims[name].(string)
	if !ok {
		return ""
	}
	return value
}

// boolClaim lê um claim booleano tolerando a forma em TEXTO ("true"), que
// alguns provedores OIDC emitem. Qualquer outra coisa é `false` — o default
// seguro: na dúvida, o e-mail não está verificado.
func boolClaim(payload *idtoken.Payload, name string) bool {
	switch value := payload.Claims[name].(type) {
	case bool:
		return value
	case string:
		return value == "true"
	default:
		return false
	}
}

// translateError converte a falha da biblioteca nos sentinels do domínio.
//
// LIMITAÇÃO CONHECIDA: a biblioteca do Google não exporta erros tipados pra
// essas condições, então distinguir "expirado" de "inválido" depende de
// inspecionar a mensagem. É frágil, e por isso o default é ErrInvalidToken:
// se a mensagem mudar numa atualização, o pior caso é o cliente receber
// "token inválido" em vez de "token expirado" — 401 nos dois casos, nenhuma
// brecha de segurança. O contrário (assumir expirado) seria pior.
func translateError(err error) error {
	if strings.Contains(strings.ToLower(err.Error()), "expired") {
		return identity.ErrTokenExpired
	}
	return identity.ErrInvalidToken
}

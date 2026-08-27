// Teste INTERNO (package googleoidc, não googleoidc_test): exercita o
// mapeamento de claims e a tradução de erro, que são não-exportados.
// Convenção do spec §8 para testar função interna.
//
// O que NÃO é testado aqui: a verificação de assinatura em si. Ela é da
// biblioteca do Google e exigiria rede e chave privada pra forjar token — o
// que este arquivo cobre é o que FAZEMOS com o resultado dela.
package googleoidc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/idtoken"

	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

// TestNewVerifierExigeAudience é o teste da falha clássica de OIDC: sem
// audience, o serviço aceitaria token legítimo do Google emitido pra OUTRO
// aplicativo — e qualquer pessoa com um app registrado autenticaria como
// qualquer usuário nosso.
func TestNewVerifierExigeAudience(t *testing.T) {
	t.Parallel()

	for _, audience := range []string{"", "   "} {
		verifier, err := NewVerifier(audience)

		require.Error(t, err, "audience vazia tem que impedir o processo de subir")
		assert.Nil(t, verifier)
	}
}

// TestVerifyPassaAudienceConfigurada garante que a audience configurada é a
// que chega na validação — um bug aqui reabriria exatamente o furo acima.
func TestVerifyPassaAudienceConfigurada(t *testing.T) {
	t.Parallel()

	var receivedAudience string
	var receivedToken string
	verifier := &Verifier{
		audience: "client-id-123",
		validate: func(_ context.Context, rawToken string, audience string) (*idtoken.Payload, error) {
			receivedToken = rawToken
			receivedAudience = audience
			return &idtoken.Payload{Subject: "sub-1", Claims: map[string]any{
				"email": "luigi@example.com", "email_verified": true, "name": "Luigi",
			}}, nil
		},
	}

	verified, err := verifier.Verify(context.Background(), "token-abc")

	require.NoError(t, err)
	assert.Equal(t, "client-id-123", receivedAudience)
	assert.Equal(t, "token-abc", receivedToken)
	assert.Equal(t, identity.ProviderGoogle, verified.Provider)
	assert.Equal(t, "sub-1", verified.Subject)
	assert.Equal(t, "luigi@example.com", verified.Email)
	assert.Equal(t, "Luigi", verified.Name)
	assert.True(t, verified.EmailVerified)
}

// TestVerifyTokenVazio: nem chega a chamar a biblioteca.
func TestVerifyTokenVazio(t *testing.T) {
	t.Parallel()

	called := false
	verifier := &Verifier{
		audience: "client-id-123",
		validate: func(context.Context, string, string) (*idtoken.Payload, error) {
			called = true
			return nil, nil
		},
	}

	_, err := verifier.Verify(context.Background(), "   ")

	require.ErrorIs(t, err, identity.ErrInvalidToken)
	assert.False(t, called)
}

// TestEmailVerifiedNaDuvidaEhFalso é a regra de segurança do mapeamento:
// claim ausente, de outro tipo, ou com valor inesperado NUNCA vira
// "verificado".
func TestEmailVerifiedNaDuvidaEhFalso(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		claim any
		want  bool
	}{
		{name: "bool true", claim: true, want: true},
		{name: "bool false", claim: false, want: false},
		{name: "texto true (alguns IdPs emitem assim)", claim: "true", want: true},
		{name: "texto false", claim: "false", want: false},
		{name: "texto inesperado", claim: "sim", want: false},
		{name: "número", claim: 1, want: false},
		{name: "nil", claim: nil, want: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			payload := &idtoken.Payload{Subject: "sub-1", Claims: map[string]any{
				"email": "luigi@example.com", "email_verified": testCase.claim,
			}}

			verified := toVerifiedIdentity(payload)

			assert.Equal(t, testCase.want, verified.EmailVerified)
		})
	}
}

// TestClaimAusenteViraVazio: o domínio é que decide se vazio é problema
// (NewEmail recusa; nome vazio cai no default do use case).
func TestClaimAusenteViraVazio(t *testing.T) {
	t.Parallel()

	verified := toVerifiedIdentity(&idtoken.Payload{Subject: "sub-1", Claims: map[string]any{}})

	assert.Equal(t, "sub-1", verified.Subject)
	assert.Empty(t, verified.Email)
	assert.Empty(t, verified.Name)
	assert.False(t, verified.EmailVerified)
}

// TestTranslateError cobre a heurística de mensagem e, principalmente, o
// DEFAULT: mensagem desconhecida vira "inválido", nunca "expirado".
func TestTranslateError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		from    error
		wantErr error
	}{
		{name: "expirado", from: errors.New("idtoken: token expired"), wantErr: identity.ErrTokenExpired},
		{name: "expirado em caixa alta", from: errors.New("Token EXPIRED at ..."), wantErr: identity.ErrTokenExpired},
		{name: "assinatura inválida", from: errors.New("idtoken: invalid signature"), wantErr: identity.ErrInvalidToken},
		{name: "audience errada", from: errors.New("idtoken: audience provided does not match aud claim"), wantErr: identity.ErrInvalidToken},
		{name: "mensagem desconhecida", from: errors.New("algo novo que a lib passou a dizer"), wantErr: identity.ErrInvalidToken},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.ErrorIs(t, translateError(testCase.from), testCase.wantErr)
		})
	}
}

// TestVerifyTraduzFalhaDaBiblioteca fecha o caminho completo do erro.
func TestVerifyTraduzFalhaDaBiblioteca(t *testing.T) {
	t.Parallel()

	verifier := &Verifier{
		audience: "client-id-123",
		validate: func(context.Context, string, string) (*idtoken.Payload, error) {
			return nil, errors.New("idtoken: token expired")
		},
	}

	_, err := verifier.Verify(context.Background(), "token-abc")

	require.ErrorIs(t, err, identity.ErrTokenExpired)
}

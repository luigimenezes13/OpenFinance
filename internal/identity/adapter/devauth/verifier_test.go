//go:build devauth

package devauth_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/devauth"
	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

func newVerifier() *devauth.Verifier {
	return devauth.NewVerifier(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// TestVerifyAceitaEmail cobre o uso pretendido: o token É o e-mail.
func TestVerifyAceitaEmail(t *testing.T) {
	verified, err := newVerifier().Verify(context.Background(), "luigi@example.com")

	require.NoError(t, err)
	assert.Equal(t, "luigi@example.com", verified.Email)
	assert.Equal(t, "luigi", verified.Name)
	assert.True(t, verified.EmailVerified)
	assert.Equal(t, identity.ProviderGoogle, verified.Provider)
}

// TestSubjectTemPrefixoDevauth: os usuários criados por aqui são
// distinguíveis dos reais no banco, e um login real do Google com o mesmo
// e-mail cria OUTRO usuário em vez de assumir este.
func TestSubjectTemPrefixoDevauth(t *testing.T) {
	verified, err := newVerifier().Verify(context.Background(), "luigi@example.com")

	require.NoError(t, err)
	assert.Equal(t, "devauth:luigi@example.com", verified.Subject)
	assert.NotEqual(t, verified.Email, verified.Subject)
}

// TestVerifyRecusaTokenQueNaoEhEmail evita o erro silencioso de mandar um
// token real e virar um usuário com e-mail bizarro.
func TestVerifyRecusaTokenQueNaoEhEmail(t *testing.T) {
	for _, rawToken := range []string{"", "   ", "eyJhbGciOiJSUzI1NiIsImtpZCI6"} {
		_, err := newVerifier().Verify(context.Background(), rawToken)

		require.ErrorIs(t, err, identity.ErrInvalidToken)
	}
}

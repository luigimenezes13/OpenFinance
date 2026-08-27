package identity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

// TestNewEmailCanonicaliza: e-mail é comparado e indexado pela forma
// canônica, senão "Luigi@Gmail.com" e "luigi@gmail.com" viram dois usuários.
func TestNewEmailCanonicaliza(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want string
	}{
		{raw: "luigi@example.com", want: "luigi@example.com"},
		{raw: "LUIGI@EXAMPLE.COM", want: "luigi@example.com"},
		{raw: "  luigi@example.com  ", want: "luigi@example.com"},
		{raw: "Luigi.Menezes+tag@example.com.br", want: "luigi.menezes+tag@example.com.br"},
	}

	for _, testCase := range cases {
		t.Run(testCase.raw, func(t *testing.T) {
			t.Parallel()

			email, err := identity.NewEmail(testCase.raw)

			require.NoError(t, err)
			assert.Equal(t, testCase.want, email.String())
		})
	}
}

// TestNewEmailRecusa cobre a validação ESTRUTURAL — não é validação de
// existência, que é papel do provedor (claim email_verified).
func TestNewEmailRecusa(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"",
		"   ",
		"sem-arroba.com",
		"@example.com",
		"luigi@",
		"luigi@example",
		"luigi@.com",
		"luigi@example.",
		"luigi@a@b.com",
		"luigi menezes@example.com",
		"luigi@exam ple.com",
	}

	for _, raw := range invalid {
		t.Run("recusa:"+raw, func(t *testing.T) {
			t.Parallel()

			email, err := identity.NewEmail(raw)

			require.ErrorIs(t, err, identity.ErrInvalidEmail)
			assert.True(t, email.IsZero())
		})
	}
}

// TestEmailEquals compara por valor, na forma canônica.
func TestEmailEquals(t *testing.T) {
	t.Parallel()

	first, err := identity.NewEmail("luigi@example.com")
	require.NoError(t, err)
	second, err := identity.NewEmail("LUIGI@example.com")
	require.NoError(t, err)
	other, err := identity.NewEmail("outro@example.com")
	require.NoError(t, err)

	assert.True(t, first.Equals(second), "a canonicalização faz dos dois o mesmo e-mail")
	assert.False(t, first.Equals(other))
}

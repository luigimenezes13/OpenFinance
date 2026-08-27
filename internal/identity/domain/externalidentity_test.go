package identity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

// TestNewExternalIdentity cobre o conjunto fechado de provedores. Provedor
// desconhecido é recusado porque aceitar qualquer string deixaria um erro de
// digitação ("gogle") criar um usuário fantasma, que nunca mais seria
// encontrado na busca por identidade — e o próximo login criaria outro.
func TestNewExternalIdentity(t *testing.T) {
	t.Parallel()

	t.Run("provedor suportado", func(t *testing.T) {
		t.Parallel()

		external, err := identity.NewExternalIdentity("google", "sub-123")

		require.NoError(t, err)
		assert.Equal(t, "google", external.Provider())
		assert.Equal(t, "sub-123", external.Subject())
	})

	t.Run("provedor em caixa alta é canonicalizado", func(t *testing.T) {
		t.Parallel()

		external, err := identity.NewExternalIdentity("  GOOGLE  ", "sub-123")

		require.NoError(t, err)
		assert.Equal(t, "google", external.Provider())
	})

	t.Run("recusa provedor desconhecido", func(t *testing.T) {
		t.Parallel()

		_, err := identity.NewExternalIdentity("facebook", "sub-123")

		require.ErrorIs(t, err, identity.ErrInvalidExternalIdentity)
	})

	t.Run("recusa subject vazio", func(t *testing.T) {
		t.Parallel()

		_, err := identity.NewExternalIdentity("google", "   ")

		require.ErrorIs(t, err, identity.ErrInvalidExternalIdentity)
	})
}

// TestExternalIdentityEquals: a busca do login compara identidade externa,
// então a igualdade tem que ser por valor nos DOIS campos.
func TestExternalIdentityEquals(t *testing.T) {
	t.Parallel()

	first, err := identity.NewExternalIdentity("google", "sub-123")
	require.NoError(t, err)
	same, err := identity.NewExternalIdentity("google", "sub-123")
	require.NoError(t, err)
	otherSubject, err := identity.NewExternalIdentity("google", "sub-456")
	require.NoError(t, err)

	assert.True(t, first.Equals(same))
	assert.False(t, first.Equals(otherSubject))
	assert.False(t, first.IsZero())
	assert.True(t, identity.ExternalIdentity{}.IsZero())
}

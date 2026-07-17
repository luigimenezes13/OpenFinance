package kernel_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testIdentity é um phantom type local só pra exercitar o TypedID genérico.
type testIdentity struct{}

const validUUID = "550e8400-e29b-41d4-a716-446655440000"

// TestNewTypedID — gerar identidade nova nunca falha e cada uma é única.
func TestNewTypedID(t *testing.T) {
	t.Parallel()

	first := kernel.NewTypedID[testIdentity]()
	second := kernel.NewTypedID[testIdentity]()

	assert.False(t, first.IsZero(), "identidade gerada não é zero value")
	assert.False(t, first.Equals(second), "cada identidade gerada é única")
}

// TestTypedIDFromUUID — a única invariante local: recusar o uuid.Nil.
func TestTypedIDFromUUID(t *testing.T) {
	t.Parallel()

	t.Run("UUID válido é aceito e faz roundtrip", func(t *testing.T) {
		t.Parallel()

		raw := uuid.MustParse(validUUID)

		got, err := kernel.TypedIDFromUUID[testIdentity](raw)

		require.NoError(t, err)
		assert.Equal(t, raw, got.UUID())
		assert.Equal(t, validUUID, got.String())
		assert.False(t, got.IsZero())
	})

	t.Run("uuid.Nil é recusado com ErrInvalidIdentifier", func(t *testing.T) {
		t.Parallel()

		_, err := kernel.TypedIDFromUUID[testIdentity](uuid.Nil)

		require.ErrorIs(t, err, kernel.ErrInvalidIdentifier)
	})
}

// TestTypedIDEquals — igualdade por valor entre identidades do mesmo tipo.
func TestTypedIDEquals(t *testing.T) {
	t.Parallel()

	raw := uuid.MustParse(validUUID)
	first, err := kernel.TypedIDFromUUID[testIdentity](raw)
	require.NoError(t, err)
	second, err := kernel.TypedIDFromUUID[testIdentity](raw)
	require.NoError(t, err)

	assert.True(t, first.Equals(second), "mesmo UUID = mesma identidade")
}

// TestTypedIDZeroValue — o zero value é reconhecido como não-identidade.
func TestTypedIDZeroValue(t *testing.T) {
	t.Parallel()

	var zero kernel.TypedID[testIdentity]

	assert.True(t, zero.IsZero())
}

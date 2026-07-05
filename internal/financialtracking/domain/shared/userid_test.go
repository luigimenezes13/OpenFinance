package shared_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// UUIDs válidos pra usar nos casos de teste.
const (
	validUUID = "550e8400-e29b-41d4-a716-446655440000"
	otherUUID = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
)

// mustUserID constrói um UserID válido ou falha o teste na hora.
// uuid.MustParse aqui é papel de borda: o teste FAZ o trabalho do mapper.
func mustUserID(t *testing.T, raw string) shared.UserID {
	t.Helper()
	userID, err := shared.NewUserID(uuid.MustParse(raw))
	require.NoError(t, err)
	return userID
}

// TestNewUserID — o domínio recebe uuid.UUID já convertido (parsing de
// string malformada é testado nos mappers das bordas, não aqui). A única
// invariante do domínio é recusar o Nil.
func TestNewUserID(t *testing.T) {
	t.Parallel()

	t.Run("UUID válido", func(t *testing.T) {
		t.Parallel()

		got, err := shared.NewUserID(uuid.MustParse(validUUID))

		require.NoError(t, err)
		assert.Equal(t, validUUID, got.String())
	})

	t.Run("uuid.Nil é recusado", func(t *testing.T) {
		t.Parallel()

		_, err := shared.NewUserID(uuid.Nil)

		require.ErrorIs(t, err, shared.ErrInvalidUserID)
	})
}

// TestUserIDEquals — igualdade por valor.
func TestUserIDEquals(t *testing.T) {
	t.Parallel()

	t.Run("mesmo UUID", func(t *testing.T) {
		t.Parallel()
		assert.True(t, mustUserID(t, validUUID).Equals(mustUserID(t, validUUID)))
	})

	t.Run("UUIDs diferentes", func(t *testing.T) {
		t.Parallel()
		assert.False(t, mustUserID(t, validUUID).Equals(mustUserID(t, otherUUID)))
	})
}

// TestUserIDIsZero — zero value vs UserID construído.
func TestUserIDIsZero(t *testing.T) {
	t.Parallel()

	t.Run("zero value", func(t *testing.T) {
		t.Parallel()
		assert.True(t, shared.UserID{}.IsZero())
	})

	t.Run("UserID construído nunca é zero", func(t *testing.T) {
		t.Parallel()
		assert.False(t, mustUserID(t, validUUID).IsZero())
	})
}

// TestUserIDUUID — o espelho pro fromDomain dos mappers: o que entra pelo
// construtor sai igual pelo UUID().
func TestUserIDUUID(t *testing.T) {
	t.Parallel()

	parsed := uuid.MustParse(validUUID)

	got, err := shared.NewUserID(parsed)

	require.NoError(t, err)
	assert.Equal(t, parsed, got.UUID())
}

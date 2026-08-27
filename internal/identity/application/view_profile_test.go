package application_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/identity/application"
	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

// TestViewProfile cobre o caminho feliz e, principalmente, o que a resposta
// NÃO contém.
func TestViewProfile(t *testing.T) {
	t.Parallel()

	existing := mustUser(t, googleIdentity())
	users := newFakeUsers(existing)
	useCase := application.NewViewProfileUseCase(users)

	output, err := useCase.Execute(context.Background(), application.ViewProfileInput{
		UserID: existing.ID().UUID(),
	})

	require.NoError(t, err)
	assert.Equal(t, existing.ID().String(), output.UserID)
	assert.Equal(t, "luigi@example.com", output.Email)
	assert.Equal(t, "Luigi Menezes", output.Name)
	assert.False(t, output.RegisteredAt.IsZero())

	// O Output é uma struct fechada, então "não vazar o subject" é garantido
	// pelo compilador. O teste registra a intenção: se alguém adicionar o
	// campo, é aqui que a decisão é revisitada.
	assert.NotContains(t, output.Email, "google-sub", "o subject do provedor não faz parte do perfil")
}

// TestViewProfileRecusa cobre id nil e usuário inexistente.
func TestViewProfileRecusa(t *testing.T) {
	t.Parallel()

	t.Run("uuid nil", func(t *testing.T) {
		t.Parallel()

		useCase := application.NewViewProfileUseCase(newFakeUsers())

		_, err := useCase.Execute(context.Background(), application.ViewProfileInput{UserID: uuid.Nil})

		require.ErrorIs(t, err, identity.ErrInvalidID)
	})

	t.Run("usuário inexistente", func(t *testing.T) {
		t.Parallel()

		useCase := application.NewViewProfileUseCase(newFakeUsers())

		_, err := useCase.Execute(context.Background(), application.ViewProfileInput{UserID: uuid.New()})

		require.ErrorIs(t, err, identity.ErrNotFound)
	})

	t.Run("falha de infraestrutura", func(t *testing.T) {
		t.Parallel()

		users := newFakeUsers()
		users.findErr = errInfra
		useCase := application.NewViewProfileUseCase(users)

		_, err := useCase.Execute(context.Background(), application.ViewProfileInput{UserID: uuid.New()})

		require.ErrorIs(t, err, errInfra)
	})
}

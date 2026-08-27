//go:build integration

package entrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/entrepo"
	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

func mustUser(t *testing.T, email string, subject string) *identity.User {
	t.Helper()

	domainEmail, err := identity.NewEmail(email)
	require.NoError(t, err)
	external, err := identity.NewExternalIdentity(identity.ProviderGoogle, subject)
	require.NoError(t, err)
	registered, err := identity.Register(domainEmail, "Luigi", external)
	require.NoError(t, err)
	return registered
}

// TestIntegrationUserRepositoryRoundTrip cobre ida e volta pelas duas
// consultas da porta.
func TestIntegrationUserRepositoryRoundTrip(t *testing.T) {
	truncateUsers(t)
	ctx := context.Background()

	repository := entrepo.NewUserRepository(testClient)
	original := mustUser(t, "luigi@example.com", "google-sub-123")

	require.NoError(t, repository.Save(ctx, original))

	byID, err := repository.FindByID(ctx, original.ID())
	require.NoError(t, err)

	originalSnapshot := original.Snapshot()
	foundSnapshot := byID.Snapshot()
	assert.Equal(t, originalSnapshot.ID, foundSnapshot.ID)
	assert.Equal(t, "luigi@example.com", foundSnapshot.Email)
	assert.Equal(t, "Luigi", foundSnapshot.Name)
	assert.Equal(t, identity.ProviderGoogle, foundSnapshot.ExternalProvider)
	assert.Equal(t, "google-sub-123", foundSnapshot.ExternalSubject)
	assert.WithinDuration(t, originalSnapshot.RegisteredAt, foundSnapshot.RegisteredAt, time.Millisecond)
	assert.Empty(t, byID.Events(), "rehidratar não emite Registered")

	// A consulta do login.
	byExternal, err := repository.FindByExternalIdentity(ctx, original.ExternalIdentity())
	require.NoError(t, err)
	assert.True(t, byExternal.ID().Equals(original.ID()))
}

// TestIntegrationUserRepositoryPrimeiroAcesso: ausência vira ErrNotFound
// limpo, que é o sinal de "primeiro acesso" para o use case.
func TestIntegrationUserRepositoryPrimeiroAcesso(t *testing.T) {
	truncateUsers(t)
	ctx := context.Background()

	repository := entrepo.NewUserRepository(testClient)
	external, err := identity.NewExternalIdentity(identity.ProviderGoogle, "desconhecido")
	require.NoError(t, err)

	found, err := repository.FindByExternalIdentity(ctx, external)

	require.ErrorIs(t, err, identity.ErrNotFound)
	assert.Nil(t, found)

	missingID, err := identity.UserIDFromUUID(uuid.New())
	require.NoError(t, err)
	found, err = repository.FindByID(ctx, missingID)
	require.ErrorIs(t, err, identity.ErrNotFound)
	assert.Nil(t, found)
}

// TestIntegrationUserRepositorySyncProfile prova que o upsert atualiza o
// PERFIL e preserva o que é imutável: a âncora e o instante do registro.
func TestIntegrationUserRepositorySyncProfile(t *testing.T) {
	truncateUsers(t)
	ctx := context.Background()

	repository := entrepo.NewUserRepository(testClient)
	target := mustUser(t, "luigi@example.com", "google-sub-123")
	require.NoError(t, repository.Save(ctx, target))
	registeredAtBefore := target.Snapshot().RegisteredAt

	newEmail, err := identity.NewEmail("novo@example.com")
	require.NoError(t, err)
	changed, err := target.SyncProfile(newEmail, "Luigi Menezes")
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, repository.Save(ctx, target))

	found, err := repository.FindByID(ctx, target.ID())
	require.NoError(t, err)

	foundSnapshot := found.Snapshot()
	assert.Equal(t, "novo@example.com", foundSnapshot.Email)
	assert.Equal(t, "Luigi Menezes", foundSnapshot.Name)
	assert.Equal(t, "google-sub-123", foundSnapshot.ExternalSubject, "a âncora não muda")
	assert.WithinDuration(t, registeredAtBefore, foundSnapshot.RegisteredAt, time.Millisecond,
		"registered_at é Immutable no schema")

	var rowCount int
	require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&rowCount))
	assert.Equal(t, 1, rowCount, "upsert atualiza a linha, não insere outra")
}

// TestIntegrationUserRepositorySubjectUnico é a proteção contra corrida de
// primeiro acesso: dois logins simultâneos do MESMO Google não podem criar
// dois usuários locais, senão o login seguinte devolveria um deles ao acaso.
func TestIntegrationUserRepositorySubjectUnico(t *testing.T) {
	truncateUsers(t)
	ctx := context.Background()

	repository := entrepo.NewUserRepository(testClient)
	require.NoError(t, repository.Save(ctx, mustUser(t, "luigi@example.com", "google-sub-123")))

	duplicate := mustUser(t, "outro@example.com", "google-sub-123")
	err := repository.Save(ctx, duplicate)

	require.Error(t, err, "o índice único (provider, subject) tem que recusar")
}

// TestIntegrationUserRepositoryEmailNaoEhUnico documenta a decisão: a
// identidade é (provedor, subject), não e-mail. Com um segundo provedor no
// futuro, a mesma pessoa pode aparecer com o mesmo endereço em dois logins
// distintos, e um unique aqui recusaria cadastro legítimo.
func TestIntegrationUserRepositoryEmailNaoEhUnico(t *testing.T) {
	truncateUsers(t)
	ctx := context.Background()

	repository := entrepo.NewUserRepository(testClient)
	require.NoError(t, repository.Save(ctx, mustUser(t, "luigi@example.com", "google-sub-123")))
	require.NoError(t, repository.Save(ctx, mustUser(t, "luigi@example.com", "google-sub-999")))

	var rowCount int
	require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&rowCount))
	assert.Equal(t, 2, rowCount)
}

// TestIntegrationUsersSchemaRejectsImpossibleState valida por SQL cru que os
// CHECKs declarados no schema do Ent chegaram ao banco.
func TestIntegrationUsersSchemaRejectsImpossibleState(t *testing.T) {
	truncateUsers(t)
	ctx := context.Background()

	const rawInsert = `
INSERT INTO users (id, email, name, external_provider, external_subject, registered_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, now(), now(), now())`

	cases := []struct {
		name     string
		email    string
		userName string
		provider string
		subject  string
	}{
		{name: "e-mail em branco", email: "   ", userName: "Luigi", provider: "google", subject: "sub-1"},
		{name: "nome em branco", email: "luigi@example.com", userName: "  ", provider: "google", subject: "sub-2"},
		{name: "provedor em branco", email: "luigi@example.com", userName: "Luigi", provider: "", subject: "sub-3"},
		{name: "subject em branco", email: "luigi@example.com", userName: "Luigi", provider: "google", subject: "   "},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := testDB.ExecContext(ctx, rawInsert,
				uuid.New(), testCase.email, testCase.userName, testCase.provider, testCase.subject)

			require.Error(t, err, "o schema tem que recusar este estado")
		})
	}
}

//go:build integration

package entrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

const rawUserInsert = `
INSERT INTO users (id, email, name, external_provider, external_subject, registered_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, now(), now(), now())`

type impossibleUser struct {
	name     string
	email    string
	userName string
	provider string
	subject  string
}

func impossibleUsers() []impossibleUser {
	return []impossibleUser{
		{name: "should reject a blank email", email: "   ", userName: "Luigi", provider: "google", subject: "sub-1"},
		{name: "should reject a blank name", email: "luigi@example.com", userName: "  ", provider: "google", subject: "sub-2"},
		{name: "should reject a blank provider", email: "luigi@example.com", userName: "Luigi", provider: "", subject: "sub-3"},
		{name: "should reject a blank subject", email: "luigi@example.com", userName: "Luigi", provider: "google", subject: "   "},
	}
}

func mustUser(t *testing.T, email string, subject string) *identity.User {
	t.Helper()

	avatar, err := identity.NewAvatarURL("https://lh3.googleusercontent.com/a/photo.jpg")
	require.NoError(t, err)
	return mustUserWithAvatar(t, email, subject, avatar)
}

func mustUserWithAvatar(t *testing.T, email string, subject string, avatar identity.AvatarURL) *identity.User {
	t.Helper()

	domainEmail, err := identity.NewEmail(email)
	require.NoError(t, err)
	external, err := identity.NewExternalIdentity(identity.ProviderGoogle, subject)
	require.NoError(t, err)
	registered, err := identity.Register(domainEmail, "Luigi", avatar, external)
	require.NoError(t, err)
	return registered
}

func TestUserRepositoryIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("should round-trip the user through both port queries, including the login lookup by external identity", func(t *testing.T) {
		truncateUsers(t)
		repository := newUserRepository(t)
		original := mustUser(t, "luigi@example.com", "google-sub-123")

		require.NoError(t, repository.Save(ctx, original))

		byID, err := repository.FindByID(ctx, original.ID())
		require.NoError(t, err)

		originalSnapshot := original.Snapshot()
		foundSnapshot := byID.Snapshot()
		assert.Equal(t, originalSnapshot.ID, foundSnapshot.ID)
		assert.Equal(t, "luigi@example.com", foundSnapshot.Email)
		assert.Equal(t, "Luigi", foundSnapshot.Name)
		assert.Equal(t, "https://lh3.googleusercontent.com/a/photo.jpg", foundSnapshot.AvatarURL)
		assert.Equal(t, identity.ProviderGoogle, foundSnapshot.ExternalProvider)
		assert.Equal(t, "google-sub-123", foundSnapshot.ExternalSubject)
		assert.WithinDuration(t, originalSnapshot.RegisteredAt, foundSnapshot.RegisteredAt, time.Millisecond)
		assert.Empty(t, byID.Events(), "rehydrating does not emit Registered")

		byExternal, err := repository.FindByExternalIdentity(ctx, original.ExternalIdentity())
		require.NoError(t, err)
		assert.True(t, byExternal.ID().Equals(original.ID()))
	})

	t.Run("should signal a first access with a clean not found error", func(t *testing.T) {
		truncateUsers(t)
		repository := newUserRepository(t)
		external, err := identity.NewExternalIdentity(identity.ProviderGoogle, "unknown")
		require.NoError(t, err)

		found, err := repository.FindByExternalIdentity(ctx, external)

		require.ErrorIs(t, err, identity.ErrNotFound)
		assert.Nil(t, found)

		missingID, err := identity.UserIDFromUUID(uuid.New())
		require.NoError(t, err)
		found, err = repository.FindByID(ctx, missingID)
		require.ErrorIs(t, err, identity.ErrNotFound)
		assert.Nil(t, found)
	})

	t.Run("should upsert the profile while preserving the external anchor and the registration instant", func(t *testing.T) {
		truncateUsers(t)
		repository := newUserRepository(t)
		target := mustUser(t, "luigi@example.com", "google-sub-123")
		require.NoError(t, repository.Save(ctx, target))
		registeredAtBefore := target.Snapshot().RegisteredAt

		newEmail, err := identity.NewEmail("new@example.com")
		require.NoError(t, err)
		changed, err := target.SyncProfile(newEmail, "Luigi Menezes", target.Avatar())
		require.NoError(t, err)
		require.True(t, changed)
		require.NoError(t, repository.Save(ctx, target))

		found, err := repository.FindByID(ctx, target.ID())
		require.NoError(t, err)

		foundSnapshot := found.Snapshot()
		assert.Equal(t, "new@example.com", foundSnapshot.Email)
		assert.Equal(t, "Luigi Menezes", foundSnapshot.Name)
		assert.Equal(t, "google-sub-123", foundSnapshot.ExternalSubject, "the anchor does not change")
		assert.WithinDuration(t, registeredAtBefore, foundSnapshot.RegisteredAt, time.Millisecond,
			"registered_at is Immutable in the schema")

		var rowCount int
		require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&rowCount))
		assert.Equal(t, 1, rowCount, "the upsert updates the row instead of inserting another")
	})

	t.Run("should reject a second local user for the same provider subject so racing first logins cannot create two", func(t *testing.T) {
		truncateUsers(t)
		repository := newUserRepository(t)
		require.NoError(t, repository.Save(ctx, mustUser(t, "luigi@example.com", "google-sub-123")))

		duplicate := mustUser(t, "other@example.com", "google-sub-123")
		err := repository.Save(ctx, duplicate)

		require.Error(t, err, "the unique (provider, subject) index must reject it")
	})

	t.Run("should accept the same email for different subjects because identity is the provider subject, not the email", func(t *testing.T) {
		truncateUsers(t)
		repository := newUserRepository(t)
		require.NoError(t, repository.Save(ctx, mustUser(t, "luigi@example.com", "google-sub-123")))
		require.NoError(t, repository.Save(ctx, mustUser(t, "luigi@example.com", "google-sub-999")))

		var rowCount int
		require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&rowCount))
		assert.Equal(t, 2, rowCount)
	})

	t.Run("should keep the schema CHECK constraints guarding raw SQL writes that bypass the domain", func(t *testing.T) {
		truncateUsers(t)

		for _, testCase := range impossibleUsers() {
			t.Run(testCase.name, func(t *testing.T) {
				_, err := testDB.ExecContext(ctx, rawUserInsert,
					uuid.New(), testCase.email, testCase.userName, testCase.provider, testCase.subject)

				require.Error(t, err, "the schema must reject this state")
			})
		}
	})

	t.Run("should store a missing avatar as an empty string, not NULL, and overwrite it through the upsert", func(t *testing.T) {
		truncateUsers(t)
		repository := newUserRepository(t)

		withoutAvatar := mustUserWithAvatar(t, "no-photo@example.com", "google-sub-no-photo", identity.AvatarURL{})
		require.NoError(t, repository.Save(ctx, withoutAvatar))

		found, err := repository.FindByID(ctx, withoutAvatar.ID())
		require.NoError(t, err)
		assert.True(t, found.Avatar().IsZero())

		var stored *string
		storedID := withoutAvatar.ID()
		require.NoError(t, testDB.QueryRowContext(ctx,
			`SELECT avatar_url FROM users WHERE id = $1`, storedID.UUID()).Scan(&stored))
		require.NotNil(t, stored, "NULL would add a third state with no meaning of its own")
		assert.Empty(t, *stored)

		avatar, err := identity.NewAvatarURL("https://lh3.googleusercontent.com/a/new.jpg")
		require.NoError(t, err)
		changed, err := found.SyncProfile(found.Email(), found.Name(), avatar)
		require.NoError(t, err)
		require.True(t, changed)
		require.NoError(t, repository.Save(ctx, found))

		reloaded, err := repository.FindByID(ctx, withoutAvatar.ID())
		require.NoError(t, err)
		assert.Equal(t, "https://lh3.googleusercontent.com/a/new.jpg", reloaded.Avatar().String())

		changed, err = reloaded.SyncProfile(reloaded.Email(), reloaded.Name(), identity.AvatarURL{})
		require.NoError(t, err)
		require.True(t, changed)
		require.NoError(t, repository.Save(ctx, reloaded))

		afterRemoval, err := repository.FindByID(ctx, withoutAvatar.ID())
		require.NoError(t, err)
		assert.True(t, afterRemoval.Avatar().IsZero(), "the upsert must overwrite with empty")
	})
}

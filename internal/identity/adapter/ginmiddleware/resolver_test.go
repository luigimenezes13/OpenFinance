package ginmiddleware_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/ginmiddleware"
	"github.com/luigimenezes13/financial-manager/internal/identity/application"
	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

// fakeUsers is the minimal repository for testing the resolver against the
// REAL use case: the target is the glue between them (error propagation and
// uuid conversion), not the use case behaviour, which has its own tests.
type fakeUsers struct {
	stored  map[string]*identity.User
	findErr error
}

func (f *fakeUsers) Save(_ context.Context, user *identity.User) error {
	if f.stored == nil {
		f.stored = map[string]*identity.User{}
	}
	external := user.ExternalIdentity()
	f.stored[external.Subject()] = user
	user.ClearEvents()
	return nil
}

func (f *fakeUsers) FindByID(context.Context, identity.UserID) (*identity.User, error) {
	return nil, identity.ErrNotFound
}

func (f *fakeUsers) FindByExternalIdentity(_ context.Context, external identity.ExternalIdentity) (*identity.User, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	found, ok := f.stored[external.Subject()]
	if !ok {
		return nil, identity.ErrNotFound
	}
	return found, nil
}

type fakeVerifier struct {
	verified identity.VerifiedIdentity
	err      error
}

func (f *fakeVerifier) Verify(context.Context, string) (identity.VerifiedIdentity, error) {
	if f.err != nil {
		return identity.VerifiedIdentity{}, f.err
	}
	return f.verified, nil
}

func googleIdentity() identity.VerifiedIdentity {
	return identity.VerifiedIdentity{
		Provider:      identity.ProviderGoogle,
		Subject:       "google-sub-123",
		Email:         "luigi@example.com",
		Name:          "Luigi",
		EmailVerified: true,
	}
}

func TestUserResolver(t *testing.T) {
	t.Run("should provision the user and return the LOCAL uuid, never the provider subject", func(t *testing.T) {
		users := &fakeUsers{}
		useCase := application.NewSignInUseCase(users, &fakeVerifier{verified: googleIdentity()})
		resolve := ginmiddleware.UserResolverFrom(useCase)

		userID, err := resolve(context.Background(), "token-abc")

		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, userID)
		assert.NotEqual(t, "google-sub-123", userID.String())
	})

	t.Run("should return the same local user for every request of a session", func(t *testing.T) {
		users := &fakeUsers{}
		useCase := application.NewSignInUseCase(users, &fakeVerifier{verified: googleIdentity()})
		resolve := ginmiddleware.UserResolverFrom(useCase)

		userID, err := resolve(context.Background(), "token-abc")
		require.NoError(t, err)
		again, err := resolve(context.Background(), "token-abc")

		require.NoError(t, err)
		assert.Equal(t, userID, again)
	})
}

func TestUserResolverError(t *testing.T) {
	errDatabaseDown := errors.New("database down")

	cases := []struct {
		name    string
		arrange func(users *fakeUsers, verifier *fakeVerifier)
		wantErr error
	}{
		{
			name:    "should return an invalid token error with a nil uuid",
			arrange: func(_ *fakeUsers, verifier *fakeVerifier) { verifier.err = identity.ErrInvalidToken },
			wantErr: identity.ErrInvalidToken,
		},
		{
			name: "should return an unverified email error with a nil uuid",
			arrange: func(_ *fakeUsers, verifier *fakeVerifier) {
				verified := googleIdentity()
				verified.EmailVerified = false
				verifier.verified = verified
			},
			wantErr: identity.ErrEmailNotVerified,
		},
		{
			name:    "should return a failure reading the user with a nil uuid",
			arrange: func(users *fakeUsers, _ *fakeVerifier) { users.findErr = errDatabaseDown },
			wantErr: errDatabaseDown,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			users := &fakeUsers{}
			verifier := &fakeVerifier{verified: googleIdentity()}
			testCase.arrange(users, verifier)
			useCase := application.NewSignInUseCase(users, verifier)
			resolve := ginmiddleware.UserResolverFrom(useCase)

			userID, err := resolve(context.Background(), "token-abc")

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Equal(t, uuid.Nil, userID, "an error never comes with a usable uuid, or the request would go on as a user that does not exist")
		})
	}
}

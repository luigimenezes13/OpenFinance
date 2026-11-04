package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/identity/application"
	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

var errInfra = errors.New("fake: infrastructure failure")

// fakeUsers behaves like the real repository: a successful Save records the
// aggregate's pending events and clears them; a failing Save keeps them.
type fakeUsers struct {
	stored      map[string]*identity.User
	saved       []*identity.User
	savedEvents []events.Event
	saveErr     error
	findErr     error
}

func newFakeUsers(stored ...*identity.User) *fakeUsers {
	fake := &fakeUsers{stored: make(map[string]*identity.User, len(stored))}
	for _, existing := range stored {
		fake.stored[externalKey(existing.ExternalIdentity())] = existing
	}
	return fake
}

func externalKey(external identity.ExternalIdentity) string {
	return external.Provider() + "|" + external.Subject()
}

func (f *fakeUsers) Save(_ context.Context, user *identity.User) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.stored[externalKey(user.ExternalIdentity())] = user
	f.saved = append(f.saved, user)
	f.savedEvents = append(f.savedEvents, user.Events()...)
	user.ClearEvents()
	return nil
}

func (f *fakeUsers) FindByID(_ context.Context, id identity.UserID) (*identity.User, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	for _, existing := range f.stored {
		if existing.ID().Equals(id) {
			return existing, nil
		}
	}
	return nil, identity.ErrNotFound
}

func (f *fakeUsers) FindByExternalIdentity(_ context.Context, external identity.ExternalIdentity) (*identity.User, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	found, ok := f.stored[externalKey(external)]
	if !ok {
		return nil, identity.ErrNotFound
	}
	return found, nil
}

type fakeVerifier struct {
	verified identity.VerifiedIdentity
	err      error

	calledWithToken string
}

func (f *fakeVerifier) Verify(_ context.Context, rawToken string) (identity.VerifiedIdentity, error) {
	f.calledWithToken = rawToken
	if f.err != nil {
		return identity.VerifiedIdentity{}, f.err
	}
	return f.verified, nil
}

func googleIdentity() identity.VerifiedIdentity {
	return identity.VerifiedIdentity{
		Provider:      identity.ProviderGoogle,
		Subject:       "google-sub-123",
		Email:         "Luigi@Example.com",
		Name:          "Luigi Menezes",
		AvatarURL:     "https://lh3.googleusercontent.com/a/photo.jpg",
		EmailVerified: true,
	}
}

func mustUser(t *testing.T, verified identity.VerifiedIdentity) *identity.User {
	t.Helper()

	email, err := identity.NewEmail(verified.Email)
	require.NoError(t, err)
	external, err := identity.NewExternalIdentity(verified.Provider, verified.Subject)
	require.NoError(t, err)
	avatar, _ := identity.NewAvatarURL(verified.AvatarURL)
	registered, err := identity.Register(email, verified.Name, avatar, external)
	require.NoError(t, err)
	registered.ClearEvents()
	return registered
}

func signIn(users *fakeUsers, verifier *fakeVerifier, rawToken string) (application.SignInOutput, error) {
	useCase := application.NewSignInUseCase(users, verifier)
	return useCase.Execute(context.Background(), application.SignInInput{RawToken: rawToken})
}

func TestSignIn(t *testing.T) {
	t.Parallel()

	t.Run("should register the user on first access because a valid token from an unknown subject means first access", func(t *testing.T) {
		t.Parallel()

		users := newFakeUsers()
		verifier := &fakeVerifier{verified: googleIdentity()}

		output, err := signIn(users, verifier, "token-abc")

		require.NoError(t, err)
		assert.True(t, output.Registered, "first access registers")
		assert.Equal(t, "luigi@example.com", output.Email, "the email is stored canonicalized")
		assert.Equal(t, "Luigi Menezes", output.Name)
		assert.NotEmpty(t, output.UserID)
		assert.Equal(t, "token-abc", verifier.calledWithToken)
		require.Len(t, users.saved, 1)
		require.Len(t, users.savedEvents, 1, "registration saves the event")
		assert.Equal(t, identity.EventTypeRegistered, users.savedEvents[0].EventName())
		assert.Empty(t, users.saved[0].Events(), "the save clears the recorded events")
	})

	t.Run("should neither register nor save when an unchanged user signs in again", func(t *testing.T) {
		t.Parallel()

		verified := googleIdentity()
		existing := mustUser(t, verified)
		users := newFakeUsers(existing)

		output, err := signIn(users, &fakeVerifier{verified: verified}, "token-abc")

		require.NoError(t, err)
		assert.False(t, output.Registered)
		assert.Equal(t, existing.ID().String(), output.UserID, "it is the SAME local user")
		assert.Empty(t, users.saved, "nothing changed, nothing to save")
		assert.Empty(t, users.savedEvents, "registration happens only once")
	})

	t.Run("should sync the profile updated at the provider and keep the local user id", func(t *testing.T) {
		t.Parallel()

		existing := mustUser(t, googleIdentity())
		users := newFakeUsers(existing)
		updated := googleIdentity()
		updated.Email = "new@example.com"
		updated.Name = "Luigi M."

		output, err := signIn(users, &fakeVerifier{verified: updated}, "token-abc")

		require.NoError(t, err)
		assert.False(t, output.Registered)
		assert.Equal(t, "new@example.com", output.Email)
		assert.Equal(t, "Luigi M.", output.Name)
		require.Len(t, users.saved, 1, "it changed, so it saves")
		assert.Equal(t, existing.ID().String(), output.UserID, "the local UserID does NOT change when the profile changes")
	})

	t.Run("should create a new user for a new subject because the email is not the user key", func(t *testing.T) {
		t.Parallel()

		existing := mustUser(t, googleIdentity())
		users := newFakeUsers(existing)
		otherSubject := googleIdentity()
		otherSubject.Subject = "google-sub-999"

		output, err := signIn(users, &fakeVerifier{verified: otherSubject}, "token-xyz")

		require.NoError(t, err)
		assert.True(t, output.Registered)
		assert.NotEqual(t, existing.ID().String(), output.UserID)
	})

	t.Run("should fall back to the email local part when the provider sends no name", func(t *testing.T) {
		t.Parallel()

		verified := googleIdentity()
		verified.Name = "   "

		output, err := signIn(newFakeUsers(), &fakeVerifier{verified: verified}, "token-abc")

		require.NoError(t, err)
		assert.Equal(t, "luigi", output.Name)
	})

	t.Run("should refuse an unverified email so nobody registers with someone else's address", func(t *testing.T) {
		t.Parallel()

		verified := googleIdentity()
		verified.EmailVerified = false
		users := newFakeUsers()

		_, err := signIn(users, &fakeVerifier{verified: verified}, "token-abc")

		require.ErrorIs(t, err, identity.ErrEmailNotVerified)
		assert.Empty(t, users.saved, "no user is created")
		assert.Empty(t, users.savedEvents)
	})

	t.Run("should sync the avatar changed at the provider", func(t *testing.T) {
		t.Parallel()

		existing := mustUser(t, googleIdentity())
		users := newFakeUsers(existing)
		updated := googleIdentity()
		updated.AvatarURL = "https://lh3.googleusercontent.com/a/new.jpg"

		_, err := signIn(users, &fakeVerifier{verified: updated}, "token-abc")

		require.NoError(t, err)
		require.Len(t, users.saved, 1, "a new avatar is a profile change, so it saves")
		avatar := users.saved[0].Avatar()
		assert.Equal(t, "https://lh3.googleusercontent.com/a/new.jpg", avatar.String())
	})

	t.Run("should clear the avatar removed at the provider", func(t *testing.T) {
		t.Parallel()

		existing := mustUser(t, googleIdentity())
		users := newFakeUsers(existing)
		withoutAvatar := googleIdentity()
		withoutAvatar.AvatarURL = ""

		_, err := signIn(users, &fakeVerifier{verified: withoutAvatar}, "token-abc")

		require.NoError(t, err)
		require.Len(t, users.saved, 1)
		avatar := users.saved[0].Avatar()
		assert.True(t, avatar.IsZero())
	})
}

func TestSignInInvalidAvatar(t *testing.T) {
	t.Parallel()

	cases := []string{"javascript:alert(1)", "/relative.png", "", "   "}

	for _, rawAvatar := range cases {
		t.Run("should sign in without an avatar because a cosmetic field never blocks access: "+rawAvatar, func(t *testing.T) {
			t.Parallel()

			verified := googleIdentity()
			verified.AvatarURL = rawAvatar
			users := newFakeUsers()

			output, err := signIn(users, &fakeVerifier{verified: verified}, "token-abc")

			require.NoError(t, err)
			assert.True(t, output.Registered)
			require.Len(t, users.saved, 1)
			avatar := users.saved[0].Avatar()
			assert.True(t, avatar.IsZero(), "an invalid avatar became absence, not an error")
		})
	}
}

func TestSignInVerificationFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		fromVerifier error
	}{
		{name: "should return an invalid token as is, since the HTTP edge picks the status", fromVerifier: identity.ErrInvalidToken},
		{name: "should return an expired token as is, since the HTTP edge picks the status", fromVerifier: identity.ErrTokenExpired},
		{name: "should return an infrastructure failure as is", fromVerifier: errInfra},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			users := newFakeUsers()

			_, err := signIn(users, &fakeVerifier{err: testCase.fromVerifier}, "token-abc")

			require.ErrorIs(t, err, testCase.fromVerifier)
			assert.Empty(t, users.saved)
		})
	}
}

func TestSignInInvalidProviderData(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutate  func(verified *identity.VerifiedIdentity)
		wantErr error
	}{
		{
			name:    "should refuse an unknown provider",
			mutate:  func(verified *identity.VerifiedIdentity) { verified.Provider = "facebook" },
			wantErr: identity.ErrInvalidExternalIdentity,
		},
		{
			name:    "should refuse a missing subject",
			mutate:  func(verified *identity.VerifiedIdentity) { verified.Subject = "" },
			wantErr: identity.ErrInvalidExternalIdentity,
		},
		{
			name:    "should refuse an invalid email",
			mutate:  func(verified *identity.VerifiedIdentity) { verified.Email = "not-an-email" },
			wantErr: identity.ErrInvalidEmail,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			verified := googleIdentity()
			testCase.mutate(&verified)
			users := newFakeUsers()

			_, err := signIn(users, &fakeVerifier{verified: verified}, "token-abc")

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, users.saved)
		})
	}
}

func TestSignInInfrastructureFailure(t *testing.T) {
	t.Parallel()

	t.Run("should return the error when reading the user fails", func(t *testing.T) {
		t.Parallel()

		users := newFakeUsers()
		users.findErr = errInfra

		_, err := signIn(users, &fakeVerifier{verified: googleIdentity()}, "token-abc")

		require.ErrorIs(t, err, errInfra)
	})

	t.Run("should save no event when saving the new user fails", func(t *testing.T) {
		t.Parallel()

		users := newFakeUsers()
		users.saveErr = errInfra

		_, err := signIn(users, &fakeVerifier{verified: googleIdentity()}, "token-abc")

		require.ErrorIs(t, err, errInfra)
		assert.Empty(t, users.saved)
		assert.Empty(t, users.savedEvents, "no event is saved when persistence fails")
	})
}

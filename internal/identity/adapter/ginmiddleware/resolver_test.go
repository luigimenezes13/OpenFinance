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
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

// Fakes mínimos: o resolvedor é testado com o use case REAL, porque o que
// interessa aqui é a cola entre os dois (propagação de erro e conversão do
// uuid), não o comportamento do use case — que tem os seus próprios testes.

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

type noopDispatcher struct{}

func (noopDispatcher) Register(string, events.Handler) {}

func (noopDispatcher) Dispatch(context.Context, ...events.Event) error { return nil }

func googleIdentity() identity.VerifiedIdentity {
	return identity.VerifiedIdentity{
		Provider:      identity.ProviderGoogle,
		Subject:       "google-sub-123",
		Email:         "luigi@example.com",
		Name:          "Luigi",
		EmailVerified: true,
	}
}

// TestUserResolverProvisionaEDevolveUUIDLocal: o resolvedor entrega ao
// middleware o uuid LOCAL — nunca o subject do provedor, que é o que
// atravessaria se a cola estivesse errada.
func TestUserResolverProvisionaEDevolveUUIDLocal(t *testing.T) {
	users := &fakeUsers{}
	useCase := application.NewSignInUseCase(users, &fakeVerifier{verified: googleIdentity()}, noopDispatcher{})
	resolve := ginmiddleware.UserResolverFrom(useCase)

	userID, err := resolve(context.Background(), "token-abc")

	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, userID)
	assert.NotEqual(t, "google-sub-123", userID.String())

	// Segunda chamada com o mesmo token devolve o MESMO usuário local: é o
	// que garante que cada request de uma sessão fala do mesmo dono.
	again, err := resolve(context.Background(), "token-abc")
	require.NoError(t, err)
	assert.Equal(t, userID, again)
}

// TestUserResolverPropagaErro: sem isso, o middleware receberia uuid.Nil sem
// erro e o request seguiria como um usuário que não existe.
func TestUserResolverPropagaErro(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(users *fakeUsers, verifier *fakeVerifier)
		wantErr error
	}{
		{
			name:    "token inválido",
			arrange: func(_ *fakeUsers, verifier *fakeVerifier) { verifier.err = identity.ErrInvalidToken },
			wantErr: identity.ErrInvalidToken,
		},
		{
			name: "e-mail não verificado",
			arrange: func(_ *fakeUsers, verifier *fakeVerifier) {
				verified := googleIdentity()
				verified.EmailVerified = false
				verifier.verified = verified
			},
			wantErr: identity.ErrEmailNotVerified,
		},
		{
			name:    "falha lendo o usuário",
			arrange: func(users *fakeUsers, _ *fakeVerifier) { users.findErr = errors.New("banco fora") },
			wantErr: nil, // erro de infra, checado abaixo
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			users := &fakeUsers{}
			verifier := &fakeVerifier{verified: googleIdentity()}
			testCase.arrange(users, verifier)

			useCase := application.NewSignInUseCase(users, verifier, noopDispatcher{})
			resolve := ginmiddleware.UserResolverFrom(useCase)

			userID, err := resolve(context.Background(), "token-abc")

			require.Error(t, err)
			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
			}
			assert.Equal(t, uuid.Nil, userID, "erro nunca vem com uuid utilizável")
		})
	}
}

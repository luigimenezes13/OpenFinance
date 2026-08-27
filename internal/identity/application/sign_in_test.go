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

// --- Fakes das portas ----------------------------------------------------

type fakeUsers struct {
	stored  map[string]*identity.User
	saved   []*identity.User
	saveErr error
	findErr error
}

func newFakeUsers(stored ...*identity.User) *fakeUsers {
	fake := &fakeUsers{stored: make(map[string]*identity.User, len(stored))}
	for _, existing := range stored {
		fake.stored[externalKey(existing.ExternalIdentity())] = existing
	}
	return fake
}

// externalKey indexa por (provedor, subject) — é a busca do login.
func externalKey(external identity.ExternalIdentity) string {
	return external.Provider() + "|" + external.Subject()
}

func (f *fakeUsers) Save(_ context.Context, user *identity.User) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.stored[externalKey(user.ExternalIdentity())] = user
	f.saved = append(f.saved, user)
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

type fakeDispatcher struct {
	dispatched  []events.Event
	dispatchErr error
}

func (f *fakeDispatcher) Register(_ string, _ events.Handler) {}

func (f *fakeDispatcher) Dispatch(_ context.Context, dispatched ...events.Event) error {
	if f.dispatchErr != nil {
		return f.dispatchErr
	}
	f.dispatched = append(f.dispatched, dispatched...)
	return nil
}

// googleIdentity monta o que o verificador devolveria num login normal.
func googleIdentity() identity.VerifiedIdentity {
	return identity.VerifiedIdentity{
		Provider:      identity.ProviderGoogle,
		Subject:       "google-sub-123",
		Email:         "Luigi@Example.com",
		Name:          "Luigi Menezes",
		EmailVerified: true,
	}
}

func mustUser(t *testing.T, verified identity.VerifiedIdentity) *identity.User {
	t.Helper()

	email, err := identity.NewEmail(verified.Email)
	require.NoError(t, err)
	external, err := identity.NewExternalIdentity(verified.Provider, verified.Subject)
	require.NoError(t, err)
	registered, err := identity.Register(email, verified.Name, external)
	require.NoError(t, err)
	registered.ClearEvents()
	return registered
}

// --- Testes --------------------------------------------------------------

// TestPrimeiroAcessoRegistra é o coração do provisionamento just-in-time:
// token válido de quem o sistema não conhece significa primeiro acesso.
func TestPrimeiroAcessoRegistra(t *testing.T) {
	t.Parallel()

	users := newFakeUsers()
	verifier := &fakeVerifier{verified: googleIdentity()}
	dispatcher := &fakeDispatcher{}
	useCase := application.NewSignInUseCase(users, verifier, dispatcher)

	output, err := useCase.Execute(context.Background(), application.SignInInput{RawToken: "token-abc"})

	require.NoError(t, err)
	assert.True(t, output.Registered, "primeiro acesso registra")
	assert.Equal(t, "luigi@example.com", output.Email, "o e-mail é guardado canonicalizado")
	assert.Equal(t, "Luigi Menezes", output.Name)
	assert.NotEmpty(t, output.UserID)

	assert.Equal(t, "token-abc", verifier.calledWithToken)
	require.Len(t, users.saved, 1)

	require.Len(t, dispatcher.dispatched, 1, "registro publica o evento")
	assert.Equal(t, identity.EventTypeRegistered, dispatcher.dispatched[0].EventName())
	assert.Empty(t, users.saved[0].Events(), "ClearEvents roda depois do dispatch")
}

// TestSegundoAcessoNaoRegistraNemGrava: login de quem já existe e não mudou
// nada é LEITURA. Gravar aqui seria uma escrita por request sem fato novo.
func TestSegundoAcessoNaoRegistraNemGrava(t *testing.T) {
	t.Parallel()

	verified := googleIdentity()
	existing := mustUser(t, verified)
	users := newFakeUsers(existing)
	dispatcher := &fakeDispatcher{}
	useCase := application.NewSignInUseCase(users, &fakeVerifier{verified: verified}, dispatcher)

	output, err := useCase.Execute(context.Background(), application.SignInInput{RawToken: "token-abc"})

	require.NoError(t, err)
	assert.False(t, output.Registered)
	assert.Equal(t, existing.ID().String(), output.UserID, "é o MESMO usuário local")
	assert.Empty(t, users.saved, "nada mudou, nada a gravar")
	assert.Empty(t, dispatcher.dispatched, "registro só acontece uma vez")
}

// TestPerfilAtualizadoNoProvedor: a pessoa trocou nome/e-mail no Google e
// espera ver refletido aqui.
func TestPerfilAtualizadoNoProvedor(t *testing.T) {
	t.Parallel()

	existing := mustUser(t, googleIdentity())
	users := newFakeUsers(existing)

	updated := googleIdentity()
	updated.Email = "novo@example.com"
	updated.Name = "Luigi M."
	useCase := application.NewSignInUseCase(users, &fakeVerifier{verified: updated}, &fakeDispatcher{})

	output, err := useCase.Execute(context.Background(), application.SignInInput{RawToken: "token-abc"})

	require.NoError(t, err)
	assert.False(t, output.Registered)
	assert.Equal(t, "novo@example.com", output.Email)
	assert.Equal(t, "Luigi M.", output.Name)
	require.Len(t, users.saved, 1, "mudou, então grava")
	assert.Equal(t, existing.ID().String(), output.UserID, "o UserID local NÃO muda quando o perfil muda")
}

// TestSubjectNovoCriaUsuarioNovo: o subject é a âncora. Mesmo e-mail com
// subject diferente é outra identidade — e-mail não é chave de usuário.
func TestSubjectNovoCriaUsuarioNovo(t *testing.T) {
	t.Parallel()

	existing := mustUser(t, googleIdentity())
	users := newFakeUsers(existing)

	otherSubject := googleIdentity()
	otherSubject.Subject = "google-sub-999"
	useCase := application.NewSignInUseCase(users, &fakeVerifier{verified: otherSubject}, &fakeDispatcher{})

	output, err := useCase.Execute(context.Background(), application.SignInInput{RawToken: "token-xyz"})

	require.NoError(t, err)
	assert.True(t, output.Registered)
	assert.NotEqual(t, existing.ID().String(), output.UserID)
}

// TestNomeAusenteUsaParteLocalDoEmail: o claim `name` depende dos escopos
// concedidos, então recusar o login por falta dele seria desproporcional.
func TestNomeAusenteUsaParteLocalDoEmail(t *testing.T) {
	t.Parallel()

	verified := googleIdentity()
	verified.Name = "   "
	users := newFakeUsers()
	useCase := application.NewSignInUseCase(users, &fakeVerifier{verified: verified}, &fakeDispatcher{})

	output, err := useCase.Execute(context.Background(), application.SignInInput{RawToken: "token-abc"})

	require.NoError(t, err)
	assert.Equal(t, "luigi", output.Name)
}

// TestEmailNaoVerificadoEhRecusado é o teste de SEGURANÇA do fluxo: sem essa
// recusa, alguém se registraria com o e-mail de outra pessoa.
func TestEmailNaoVerificadoEhRecusado(t *testing.T) {
	t.Parallel()

	verified := googleIdentity()
	verified.EmailVerified = false
	users := newFakeUsers()
	dispatcher := &fakeDispatcher{}
	useCase := application.NewSignInUseCase(users, &fakeVerifier{verified: verified}, dispatcher)

	_, err := useCase.Execute(context.Background(), application.SignInInput{RawToken: "token-abc"})

	require.ErrorIs(t, err, identity.ErrEmailNotVerified)
	assert.Empty(t, users.saved, "usuário nenhum é criado")
	assert.Empty(t, dispatcher.dispatched)
}

// TestFalhaDeVerificacaoSobeComoVeio: os sentinels do verificador chegam
// intactos, porque é a borda HTTP que decide o status (401 vs 401 com
// "renove o token").
func TestFalhaDeVerificacaoSobeComoVeio(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		fromVerifier error
	}{
		{name: "token inválido", fromVerifier: identity.ErrInvalidToken},
		{name: "token expirado", fromVerifier: identity.ErrTokenExpired},
		{name: "falha de infraestrutura", fromVerifier: errInfra},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			users := newFakeUsers()
			useCase := application.NewSignInUseCase(users, &fakeVerifier{err: testCase.fromVerifier}, &fakeDispatcher{})

			_, err := useCase.Execute(context.Background(), application.SignInInput{RawToken: "token-abc"})

			require.ErrorIs(t, err, testCase.fromVerifier)
			assert.Empty(t, users.saved)
		})
	}
}

// TestDadoInvalidoDoProvedor: o provedor é fronteira, não fonte de verdade
// do domínio. Dado incoerente vindo dele morre nos VOs.
func TestDadoInvalidoDoProvedor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutate  func(verified *identity.VerifiedIdentity)
		wantErr error
	}{
		{
			name:    "provedor desconhecido",
			mutate:  func(v *identity.VerifiedIdentity) { v.Provider = "facebook" },
			wantErr: identity.ErrInvalidExternalIdentity,
		},
		{
			name:    "subject ausente",
			mutate:  func(v *identity.VerifiedIdentity) { v.Subject = "" },
			wantErr: identity.ErrInvalidExternalIdentity,
		},
		{
			name:    "e-mail inválido",
			mutate:  func(v *identity.VerifiedIdentity) { v.Email = "nao-e-email" },
			wantErr: identity.ErrInvalidEmail,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			verified := googleIdentity()
			testCase.mutate(&verified)
			users := newFakeUsers()
			useCase := application.NewSignInUseCase(users, &fakeVerifier{verified: verified}, &fakeDispatcher{})

			_, err := useCase.Execute(context.Background(), application.SignInInput{RawToken: "token-abc"})

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, users.saved)
		})
	}
}

// TestPropagaErroDeInfra cobre as fronteiras de persistência e de publicação.
func TestPropagaErroDeInfra(t *testing.T) {
	t.Parallel()

	t.Run("falha lendo o usuário", func(t *testing.T) {
		t.Parallel()

		users := newFakeUsers()
		users.findErr = errInfra
		useCase := application.NewSignInUseCase(users, &fakeVerifier{verified: googleIdentity()}, &fakeDispatcher{})

		_, err := useCase.Execute(context.Background(), application.SignInInput{RawToken: "token-abc"})

		require.ErrorIs(t, err, errInfra)
	})

	t.Run("falha gravando o usuário novo", func(t *testing.T) {
		t.Parallel()

		users := newFakeUsers()
		users.saveErr = errInfra
		dispatcher := &fakeDispatcher{}
		useCase := application.NewSignInUseCase(users, &fakeVerifier{verified: googleIdentity()}, dispatcher)

		_, err := useCase.Execute(context.Background(), application.SignInInput{RawToken: "token-abc"})

		require.ErrorIs(t, err, errInfra)
		assert.Empty(t, dispatcher.dispatched, "evento não sai se a persistência falhou")
	})

	t.Run("falha publicando o evento", func(t *testing.T) {
		t.Parallel()

		users := newFakeUsers()
		dispatcher := &fakeDispatcher{dispatchErr: errInfra}
		useCase := application.NewSignInUseCase(users, &fakeVerifier{verified: googleIdentity()}, dispatcher)

		_, err := useCase.Execute(context.Background(), application.SignInInput{RawToken: "token-abc"})

		require.ErrorIs(t, err, errInfra)
		require.Len(t, users.saved, 1, "o Save já tinha acontecido")
		assert.Len(t, users.saved[0].Events(), 1, "evento não despachado fica no aggregate")
	})
}

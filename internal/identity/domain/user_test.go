package identity_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

func mustEmail(t *testing.T, raw string) identity.Email {
	t.Helper()
	email, err := identity.NewEmail(raw)
	require.NoError(t, err)
	return email
}

func mustExternalIdentity(t *testing.T, subject string) identity.ExternalIdentity {
	t.Helper()
	external, err := identity.NewExternalIdentity(identity.ProviderGoogle, subject)
	require.NoError(t, err)
	return external
}

func mustAvatar(t *testing.T, raw string) identity.AvatarURL {
	t.Helper()
	avatar, err := identity.NewAvatarURL(raw)
	require.NoError(t, err)
	return avatar
}

func newValidUser(t *testing.T) *identity.User {
	t.Helper()
	registered, err := identity.Register(
		mustEmail(t, "luigi@example.com"),
		"Luigi",
		mustAvatar(t, "https://lh3.googleusercontent.com/a/foto.jpg"),
		mustExternalIdentity(t, "google-sub-123"),
	)
	require.NoError(t, err)
	return registered
}

// TestRegister cobre o caminho feliz e a emissão do evento.
func TestRegister(t *testing.T) {
	t.Parallel()

	registered := newValidUser(t)

	assert.Equal(t, "luigi@example.com", registered.Email().String())
	assert.Equal(t, "Luigi", registered.Name())
	assert.False(t, registered.ID().IsZero())
	assert.False(t, registered.RegisteredAt().IsZero())

	recorded := registered.Events()
	require.Len(t, recorded, 1, "registrar é fato de domínio e emite evento")
	assert.Equal(t, identity.EventTypeRegistered, recorded[0].EventName())
}

// TestRegisteredNaoCarregaSubject é o teste do ENCAPSULAMENTO da referência
// externa: o evento é contrato público entre contextos, e vazar o subject
// por ele desfaria a razão de o VO ExternalIdentity existir.
func TestRegisteredNaoCarregaSubject(t *testing.T) {
	t.Parallel()

	registered := newValidUser(t)
	recorded := registered.Events()
	require.Len(t, recorded, 1)

	event, ok := recorded[0].(identity.Registered)
	require.True(t, ok)

	assert.Equal(t, identity.ProviderGoogle, event.Provider())
	assert.Equal(t, "luigi@example.com", event.Email().String())
	assert.True(t, event.UserID().Equals(registered.ID()))
}

// TestRegisterRecusa cobre as invariantes do construtor.
func TestRegisterRecusa(t *testing.T) {
	t.Parallel()

	validEmail := mustEmail(t, "luigi@example.com")
	validExternal := mustExternalIdentity(t, "google-sub-123")

	cases := []struct {
		name     string
		email    identity.Email
		userName string
		external identity.ExternalIdentity
		wantErr  error
	}{
		{name: "e-mail zero value", email: identity.Email{}, userName: "Luigi", external: validExternal, wantErr: identity.ErrInvalidEmail},
		{name: "identidade externa zero value", email: validEmail, userName: "Luigi", external: identity.ExternalIdentity{}, wantErr: identity.ErrInvalidExternalIdentity},
		{name: "nome vazio", email: validEmail, userName: "   ", external: validExternal, wantErr: identity.ErrInvalidName},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			registered, err := identity.Register(testCase.email, testCase.userName, identity.AvatarURL{}, testCase.external)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Nil(t, registered)
		})
	}
}

// TestSyncProfile cobre a sincronização de perfil e o `changed` que evita
// escrita sem fato novo.
func TestSyncProfile(t *testing.T) {
	t.Parallel()

	t.Run("perfil igual não muda nada", func(t *testing.T) {
		t.Parallel()
		target := newValidUser(t)

		changed, err := target.SyncProfile(mustEmail(t, "LUIGI@example.com "), "Luigi", target.Avatar())

		require.NoError(t, err)
		assert.False(t, changed, "e-mail canônico igual e nome igual: nada a gravar")
	})

	t.Run("e-mail novo no provedor", func(t *testing.T) {
		t.Parallel()
		target := newValidUser(t)

		changed, err := target.SyncProfile(mustEmail(t, "novo@example.com"), "Luigi", target.Avatar())

		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, "novo@example.com", target.Email().String())
	})

	t.Run("nome novo no provedor", func(t *testing.T) {
		t.Parallel()
		target := newValidUser(t)

		changed, err := target.SyncProfile(mustEmail(t, "luigi@example.com"), "Luigi Menezes", target.Avatar())

		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, "Luigi Menezes", target.Name())
	})

	t.Run("nome inválido é recusado sem mutar", func(t *testing.T) {
		t.Parallel()
		target := newValidUser(t)

		changed, err := target.SyncProfile(mustEmail(t, "novo@example.com"), "  ", target.Avatar())

		require.ErrorIs(t, err, identity.ErrInvalidName)
		assert.False(t, changed)
		assert.Equal(t, "luigi@example.com", target.Email().String(), "recusa não muta o e-mail")
	})

	t.Run("sincronizar perfil não emite evento", func(t *testing.T) {
		t.Parallel()
		target := newValidUser(t)
		target.ClearEvents()

		_, err := target.SyncProfile(mustEmail(t, "novo@example.com"), "Outro Nome", target.Avatar())

		require.NoError(t, err)
		assert.Empty(t, target.Events(), "nenhum consumidor se importa com troca de perfil no v1")
	})
}

// TestSyncProfileAvatar cobre as três transições do avatar, incluindo a
// remoção — se a pessoa apagou a foto no Google, mostrar a antiga seria
// exibir algo que ela decidiu apagar.
func TestSyncProfileAvatar(t *testing.T) {
	t.Parallel()

	t.Run("troca de avatar", func(t *testing.T) {
		t.Parallel()
		target := newValidUser(t)

		changed, err := target.SyncProfile(target.Email(), target.Name(), mustAvatar(t, "https://lh3.googleusercontent.com/a/nova.jpg"))

		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, "https://lh3.googleusercontent.com/a/nova.jpg", target.Avatar().String())
	})

	t.Run("remoção do avatar sobrescreve o anterior", func(t *testing.T) {
		t.Parallel()
		target := newValidUser(t)

		changed, err := target.SyncProfile(target.Email(), target.Name(), identity.AvatarURL{})

		require.NoError(t, err)
		assert.True(t, changed)
		assert.True(t, target.Avatar().IsZero())
	})

	t.Run("mesmo avatar não conta como mudança", func(t *testing.T) {
		t.Parallel()
		target := newValidUser(t)

		changed, err := target.SyncProfile(target.Email(), target.Name(), target.Avatar())

		require.NoError(t, err)
		assert.False(t, changed)
	})
}

// TestRegisterSemAvatar: usuário sem foto é estado legítimo, não erro.
func TestRegisterSemAvatar(t *testing.T) {
	t.Parallel()

	registered, err := identity.Register(
		mustEmail(t, "luigi@example.com"),
		"Luigi",
		identity.AvatarURL{},
		mustExternalIdentity(t, "google-sub-123"),
	)

	require.NoError(t, err)
	assert.True(t, registered.Avatar().IsZero())
	assert.Empty(t, registered.Avatar().String())
}

// TestSnapshotRoundTrip é a propriedade central do Memento.
func TestSnapshotRoundTrip(t *testing.T) {
	t.Parallel()

	original := newValidUser(t)

	rebuilt, err := identity.FromSnapshot(original.Snapshot())

	require.NoError(t, err)
	assert.Equal(t, original.Snapshot(), rebuilt.Snapshot())
	assert.True(t, rebuilt.ID().Equals(original.ID()))
	assert.Empty(t, rebuilt.Events(), "rehidratar não é registrar de novo")
}

// TestFromSnapshotRecusaEstadoCorrompido cobre as linhas impossíveis.
func TestFromSnapshotRecusaEstadoCorrompido(t *testing.T) {
	t.Parallel()

	valid := newValidUser(t).Snapshot()

	cases := []struct {
		name    string
		corrupt func(snapshot *identity.UserSnapshot)
		wantErr error
	}{
		{name: "id nil", corrupt: func(s *identity.UserSnapshot) { s.ID = uuid.Nil }, wantErr: identity.ErrInvalidID},
		{name: "e-mail inválido", corrupt: func(s *identity.UserSnapshot) { s.Email = "nao-e-email" }, wantErr: identity.ErrInvalidEmail},
		{name: "nome vazio", corrupt: func(s *identity.UserSnapshot) { s.Name = "" }, wantErr: identity.ErrInvalidName},
		{name: "provedor desconhecido", corrupt: func(s *identity.UserSnapshot) { s.ExternalProvider = "facebook" }, wantErr: identity.ErrInvalidExternalIdentity},
		{name: "subject ausente", corrupt: func(s *identity.UserSnapshot) { s.ExternalSubject = "" }, wantErr: identity.ErrInvalidExternalIdentity},
		{name: "instante de registro zerado", corrupt: func(s *identity.UserSnapshot) { s.RegisteredAt = time.Time{} }, wantErr: identity.ErrInvalidRegisteredAt},
		{name: "avatar com esquema perigoso", corrupt: func(s *identity.UserSnapshot) { s.AvatarURL = "javascript:alert(1)" }, wantErr: identity.ErrInvalidAvatarURL},
		{name: "avatar relativo", corrupt: func(s *identity.UserSnapshot) { s.AvatarURL = "/fotos/luigi.png" }, wantErr: identity.ErrInvalidAvatarURL},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			corrupted := valid
			testCase.corrupt(&corrupted)

			rebuilt, err := identity.FromSnapshot(corrupted)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Nil(t, rebuilt)
		})
	}
}

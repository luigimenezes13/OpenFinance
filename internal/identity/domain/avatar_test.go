package identity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

// TestNewAvatarURLAceita cobre as formas válidas.
func TestNewAvatarURLAceita(t *testing.T) {
	t.Parallel()

	valid := []string{
		"https://lh3.googleusercontent.com/a/ACg8ocK=s96-c",
		"http://localhost:3000/avatar.png",
		"https://cdn.example.com/u/1/foto.jpg?v=2",
		"  https://example.com/foto.png  ",
	}

	for _, raw := range valid {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			avatar, err := identity.NewAvatarURL(raw)

			require.NoError(t, err)
			assert.False(t, avatar.IsZero())
			assert.NotEmpty(t, avatar.String())
		})
	}
}

// TestNewAvatarURLRecusaEsquemaPerigoso é o teste de SEGURANÇA do VO. A URL
// vem de fora e o frontend a coloca num atributo src — sem o conjunto fechado
// de esquemas, `javascript:` ou `data:` seriam XSS entregue pela nossa API.
func TestNewAvatarURLRecusaEsquemaPerigoso(t *testing.T) {
	t.Parallel()

	dangerous := []string{
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
		"file:///etc/passwd",
		"ftp://example.com/foto.png",
	}

	for _, raw := range dangerous {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			avatar, err := identity.NewAvatarURL(raw)

			require.ErrorIs(t, err, identity.ErrInvalidAvatarURL)
			assert.True(t, avatar.IsZero())
		})
	}
}

// TestNewAvatarURLRecusaFormaInvalida cobre o resto: vazio, relativo, sem
// host.
func TestNewAvatarURLRecusaFormaInvalida(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"",
		"   ",
		"/fotos/luigi.png",
		"foto.png",
		"https://",
		"lh3.googleusercontent.com/foto.jpg",
	}

	for _, raw := range invalid {
		t.Run("recusa:"+raw, func(t *testing.T) {
			t.Parallel()

			_, err := identity.NewAvatarURL(raw)

			require.ErrorIs(t, err, identity.ErrInvalidAvatarURL)
		})
	}
}

// TestAvatarZeroValueEhSemAvatar: ausência é estado legítimo, expressa pelo
// zero value — não é erro nem exige ponteiro.
func TestAvatarZeroValueEhSemAvatar(t *testing.T) {
	t.Parallel()

	var absent identity.AvatarURL

	assert.True(t, absent.IsZero())
	assert.Empty(t, absent.String())

	present, err := identity.NewAvatarURL("https://example.com/foto.png")
	require.NoError(t, err)
	assert.False(t, present.Equals(absent))
	assert.True(t, present.Equals(present))
}

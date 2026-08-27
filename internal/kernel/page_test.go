package kernel_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

// TestNewPage cobre as três decisões do VO: default, teto e recusa.
func TestNewPage(t *testing.T) {
	t.Parallel()

	t.Run("limite zero recebe o default", func(t *testing.T) {
		t.Parallel()

		page, err := kernel.NewPage(0, 0)

		require.NoError(t, err)
		assert.Equal(t, kernel.DefaultLimit, page.Limit(), "zero é 'não pedi nada', não pedido inválido")
		assert.Zero(t, page.Offset())
	})

	t.Run("limite no teto é aceito", func(t *testing.T) {
		t.Parallel()

		page, err := kernel.NewPage(kernel.MaxLimit, 100)

		require.NoError(t, err)
		assert.Equal(t, kernel.MaxLimit, page.Limit())
		assert.Equal(t, 100, page.Offset())
	})

	t.Run("acima do teto é ERRO, não corte silencioso", func(t *testing.T) {
		t.Parallel()

		// Quem pede 1000 e recebe 200 sem aviso pagina errado e perde
		// registros sem perceber.
		page, err := kernel.NewPage(kernel.MaxLimit+1, 0)

		require.ErrorIs(t, err, kernel.ErrInvalidPage)
		assert.True(t, page.IsZero())
	})

	t.Run("valores negativos são recusados", func(t *testing.T) {
		t.Parallel()

		_, err := kernel.NewPage(-1, 0)
		require.ErrorIs(t, err, kernel.ErrInvalidPage)

		_, err = kernel.NewPage(10, -1)
		require.ErrorIs(t, err, kernel.ErrInvalidPage)
	})
}

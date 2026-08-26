package account_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// TestSnapshotRoundTrip é o teste mais importante do par Memento: fotografar
// e remontar tem que devolver o MESMO estado. É essa propriedade que
// garante que o repository não perde nem inventa dado.
func TestSnapshotRoundTrip(t *testing.T) {
	t.Parallel()

	original := newValidAccount(t)
	require.NoError(t, original.UpdateBalance(mustBalance(t, -1_250_00, "BRL", time.Now())))

	rebuilt, err := account.FromSnapshot(original.Snapshot())

	require.NoError(t, err)
	assert.Equal(t, original.Snapshot(), rebuilt.Snapshot(), "snapshot → aggregate → snapshot é idempotente")
	assert.True(t, rebuilt.ID().Equals(original.ID()), "a identidade é preservada: é a MESMA conta, não uma nova")
	assert.Empty(t, rebuilt.Events(), "rehidratar não é fato de domínio — nenhum evento é emitido")
}

// TestSnapshotRoundTripContaConectada cobre o outro caminho do VO Source: a
// referência do provider tem que sobreviver à ida e volta.
func TestSnapshotRoundTripContaConectada(t *testing.T) {
	t.Parallel()

	source, err := account.NewOpenFinanceSource("pluggy", "pluggy-acc-77")
	require.NoError(t, err)
	original, err := account.New(mustUserID(t, validUUID), "Conta Conectada", account.KindCreditCard, mustCurrency(t, "BRL"), source)
	require.NoError(t, err)

	rebuilt, err := account.FromSnapshot(original.Snapshot())
	require.NoError(t, err)

	rebuiltSource := rebuilt.Source()
	assert.True(t, rebuiltSource.IsOpenFinance())
	provider, ok := rebuiltSource.Provider()
	require.True(t, ok)
	assert.Equal(t, "pluggy", provider)
	providerAccountID, ok := rebuiltSource.ProviderAccountID()
	require.True(t, ok)
	assert.Equal(t, "pluggy-acc-77", providerAccountID)
}

// TestFromSnapshotRecusaEstadoCorrompido: linha estragada no banco não vira
// aggregate inválido. Cada campo é revalidado pelo construtor do seu VO.
func TestFromSnapshotRecusaEstadoCorrompido(t *testing.T) {
	t.Parallel()

	valid := newValidAccount(t).Snapshot()

	cases := []struct {
		name    string
		corrupt func(snapshot *account.AccountSnapshot)
		wantErr error
	}{
		{
			name:    "id nil",
			corrupt: func(snapshot *account.AccountSnapshot) { snapshot.ID = uuid.Nil },
			wantErr: account.ErrInvalidID,
		},
		{
			name:    "usuário nil",
			corrupt: func(snapshot *account.AccountSnapshot) { snapshot.UserID = uuid.Nil },
			wantErr: shared.ErrInvalidUserID,
		},
		{
			name:    "nome vazio",
			corrupt: func(snapshot *account.AccountSnapshot) { snapshot.Name = "   " },
			wantErr: account.ErrInvalidName,
		},
		{
			name:    "kind desconhecido",
			corrupt: func(snapshot *account.AccountSnapshot) { snapshot.Kind = "cripto" },
			wantErr: account.ErrInvalidKind,
		},
		{
			name:    "moeda desconhecida",
			corrupt: func(snapshot *account.AccountSnapshot) { snapshot.BalanceCurrency = "JPY" },
			wantErr: shared.ErrInvalidCurrency,
		},
		{
			name:    "instante do saldo zerado",
			corrupt: func(snapshot *account.AccountSnapshot) { snapshot.BalanceAsOf = time.Time{} },
			wantErr: account.ErrInvalidBalance,
		},
		{
			name: "openfinance sem referência",
			corrupt: func(snapshot *account.AccountSnapshot) {
				snapshot.SourceProvider = "pluggy"
				snapshot.SourceProviderAccountID = ""
			},
			wantErr: account.ErrInvalidSource,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			corrupted := valid
			testCase.corrupt(&corrupted)

			rebuilt, err := account.FromSnapshot(corrupted)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Nil(t, rebuilt)
		})
	}
}

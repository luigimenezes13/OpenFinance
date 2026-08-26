package transaction_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

// TestSnapshotRoundTrip cobre o lançamento manual simples.
func TestSnapshotRoundTrip(t *testing.T) {
	t.Parallel()

	original := newValidManualTransaction(t)

	rebuilt, err := transaction.FromSnapshot(original.Snapshot())

	require.NoError(t, err)
	assert.Equal(t, original.Snapshot(), rebuilt.Snapshot())
	assert.True(t, rebuilt.ID().Equals(original.ID()))
	assert.Empty(t, rebuilt.Events(), "rehidratar não emite evento")
	_, hasCategory := rebuilt.Category()
	assert.False(t, hasCategory, "não categorizada continua não categorizada")
	_, hasRef := rebuilt.ExternalRef()
	assert.False(t, hasRef, "manual continua manual")
}

// TestSnapshotRoundTripImportadaCategorizadaConciliada é o caso completo:
// referência externa + atribuição de categoria + conciliada. Importa
// especialmente o assignedAt: ele vem do BANCO, não de time.Now().
func TestSnapshotRoundTripImportadaCategorizadaConciliada(t *testing.T) {
	t.Parallel()

	original, err := transaction.NewFromProvider(
		mustUserID(t, validUUID),
		account.NewAccountID(),
		mustMoney(t, -89_90, "BRL"),
		time.Now().Add(-48*time.Hour),
		"Mercado Livre",
		mustExternalRef(t),
	)
	require.NoError(t, err)
	require.NoError(t, original.Categorize(category.NewCategoryID(), transaction.AssignedByRule))
	require.NoError(t, original.MarkReconciled())

	snapshot := original.Snapshot()
	rebuilt, err := transaction.FromSnapshot(snapshot)
	require.NoError(t, err)

	assert.Equal(t, snapshot, rebuilt.Snapshot())
	assert.True(t, rebuilt.IsReconciled())
	assert.Empty(t, rebuilt.Events(), "transação importada NÃO re-emite Imported ao ser lida")

	assignment, hasCategory := rebuilt.Category()
	require.True(t, hasCategory)
	assignedBy := assignment.By()
	assert.Equal(t, "rule", assignedBy.String())
	assert.Equal(t, snapshot.CategoryAssignedAt, assignment.AssignedAt(), "o instante da atribuição vem do banco, não de agora")
}

// TestFromSnapshotAceitaDataFutura documenta uma decisão de fronteira:
// "data não pode ser futura" vale na ESCRITA (NewManual recusa), não na
// leitura. Aplicar na leitura tornaria um lançamento gravado com relógio
// adiantado ilegível pra sempre.
func TestFromSnapshotAceitaDataFutura(t *testing.T) {
	t.Parallel()

	snapshot := newValidManualTransaction(t).Snapshot()
	snapshot.OccurredAt = time.Now().Add(24 * time.Hour)

	rebuilt, err := transaction.FromSnapshot(snapshot)

	require.NoError(t, err)
	assert.Equal(t, snapshot.OccurredAt, rebuilt.OccurredAt())
}

// TestFromSnapshotRecusaEstadoCorrompido cobre as linhas impossíveis.
func TestFromSnapshotRecusaEstadoCorrompido(t *testing.T) {
	t.Parallel()

	valid := newValidManualTransaction(t).Snapshot()
	nilUUID := uuid.Nil
	categoryID := uuid.New()

	cases := []struct {
		name    string
		corrupt func(snapshot *transaction.TransactionSnapshot)
		wantErr error
	}{
		{
			name:    "id nil",
			corrupt: func(snapshot *transaction.TransactionSnapshot) { snapshot.ID = uuid.Nil },
			wantErr: transaction.ErrInvalidID,
		},
		{
			name:    "usuário nil",
			corrupt: func(snapshot *transaction.TransactionSnapshot) { snapshot.UserID = uuid.Nil },
			wantErr: shared.ErrInvalidUserID,
		},
		{
			name:    "quantia zero",
			corrupt: func(snapshot *transaction.TransactionSnapshot) { snapshot.Amount = 0 },
			wantErr: transaction.ErrZeroMoney,
		},
		{
			name:    "moeda desconhecida",
			corrupt: func(snapshot *transaction.TransactionSnapshot) { snapshot.Currency = "JPY" },
			wantErr: shared.ErrInvalidCurrency,
		},
		{
			name:    "descrição vazia",
			corrupt: func(snapshot *transaction.TransactionSnapshot) { snapshot.Description = "  " },
			wantErr: transaction.ErrInvalidDescription,
		},
		{
			name:    "instante zerado",
			corrupt: func(snapshot *transaction.TransactionSnapshot) { snapshot.OccurredAt = time.Time{} },
			wantErr: transaction.ErrInvalidOccurredAt,
		},
		{
			name: "categoria nil apontada",
			corrupt: func(snapshot *transaction.TransactionSnapshot) {
				snapshot.CategoryID = &nilUUID
				snapshot.CategoryAssignedBy = "user"
				snapshot.CategoryAssignedAt = time.Now()
			},
			wantErr: category.ErrInvalidID,
		},
		{
			name: "quem atribuiu fora do vocabulário",
			corrupt: func(snapshot *transaction.TransactionSnapshot) {
				snapshot.CategoryID = &categoryID
				snapshot.CategoryAssignedBy = "robot"
				snapshot.CategoryAssignedAt = time.Now()
			},
			wantErr: transaction.ErrInvalidAssignment,
		},
		{
			name: "atribuição sem instante",
			corrupt: func(snapshot *transaction.TransactionSnapshot) {
				snapshot.CategoryID = &categoryID
				snapshot.CategoryAssignedBy = "user"
				snapshot.CategoryAssignedAt = time.Time{}
			},
			wantErr: transaction.ErrInvalidAssignment,
		},
		{
			name: "referência externa sem id no provider",
			corrupt: func(snapshot *transaction.TransactionSnapshot) {
				snapshot.ExternalRefProvider = "pluggy"
				snapshot.ExternalRefProviderTransactionID = ""
			},
			wantErr: transaction.ErrInvalidRef,
		},
		{
			name: "manual marcada como conciliada",
			corrupt: func(snapshot *transaction.TransactionSnapshot) {
				snapshot.Reconciled = true
			},
			wantErr: transaction.ErrNotReconcilable,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			corrupted := valid
			testCase.corrupt(&corrupted)

			rebuilt, err := transaction.FromSnapshot(corrupted)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Nil(t, rebuilt)
		})
	}
}

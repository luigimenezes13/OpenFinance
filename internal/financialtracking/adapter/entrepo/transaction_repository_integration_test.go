//go:build integration

package entrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

// TestIntegrationTransactionRepositoryManualRoundTrip cobre o lançamento
// manual: as colunas opcionais têm que voltar AUSENTES, não zeradas — é o
// teste que pega mapper de nullable errado.
func TestIntegrationTransactionRepositoryManualRoundTrip(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewTransactionRepository(testClient)
	occurredAt := time.Now().Add(-3 * time.Hour)
	original, err := transaction.NewManual(
		mustUserID(t, uuid.New()),
		account.NewAccountID(),
		mustMoneyBRL(t, -45_50),
		occurredAt,
		"Padaria",
	)
	require.NoError(t, err)

	require.NoError(t, repository.Save(ctx, original))

	found, err := repository.FindByID(ctx, original.ID())
	require.NoError(t, err)

	foundMoney := found.Money()
	assert.Equal(t, int64(-45_50), foundMoney.Amount())
	assert.Equal(t, "Padaria", found.Description())
	assert.WithinDuration(t, occurredAt, found.OccurredAt(), time.Millisecond)
	assert.False(t, found.IsReconciled())

	_, hasCategory := found.Category()
	assert.False(t, hasCategory, "NULL no banco vira 'não categorizada', não atribuição zerada")
	_, hasRef := found.ExternalRef()
	assert.False(t, hasRef, "transação manual não tem referência externa")
	assert.Empty(t, found.Events(), "rehidratar não emite evento")

	// Confirmação por SQL: as colunas do bloco de categoria estão NULL de
	// verdade. Sem isso, o teste passaria mesmo se o mapper gravasse "ano 1".
	var categoryID *uuid.UUID
	var assignedAt *time.Time
	originalID := original.ID()
	require.NoError(t, testDB.QueryRowContext(ctx,
		`SELECT category_id, category_assigned_at FROM transactions WHERE id = $1`, originalID.UUID()).
		Scan(&categoryID, &assignedAt))
	assert.Nil(t, categoryID)
	assert.Nil(t, assignedAt)
}

// TestIntegrationTransactionRepositoryImportedRoundTrip cobre o caso
// completo: referência externa + categoria + conciliada.
func TestIntegrationTransactionRepositoryImportedRoundTrip(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewTransactionRepository(testClient)
	ref, err := transaction.NewExternalRef("pluggy", "tx-abc-123")
	require.NoError(t, err)
	original, err := transaction.NewFromProvider(
		mustUserID(t, uuid.New()),
		account.NewAccountID(),
		mustMoneyBRL(t, -89_90),
		time.Now().Add(-48*time.Hour),
		"Mercado Livre",
		ref,
	)
	require.NoError(t, err)

	categoryID := category.NewCategoryID()
	require.NoError(t, original.Categorize(categoryID, transaction.AssignedByRule))
	require.NoError(t, original.MarkReconciled())

	require.NoError(t, repository.Save(ctx, original))

	found, err := repository.FindByID(ctx, original.ID())
	require.NoError(t, err)

	assert.True(t, found.IsReconciled())

	foundRef, hasRef := found.ExternalRef()
	require.True(t, hasRef)
	assert.Equal(t, "pluggy", foundRef.Provider())
	assert.Equal(t, "tx-abc-123", foundRef.ProviderTransactionID())

	assignment, hasCategory := found.Category()
	require.True(t, hasCategory)
	foundCategoryID := assignment.CategoryID()
	assert.True(t, foundCategoryID.Equals(categoryID))
	assignedBy := assignment.By()
	assert.Equal(t, "rule", assignedBy.String())

	originalSnapshot := original.Snapshot()
	assert.WithinDuration(t, originalSnapshot.CategoryAssignedAt, assignment.AssignedAt(), time.Millisecond,
		"o instante da atribuição vem do banco, não de time.Now() na leitura")

	assert.Len(t, original.Events(), 3, "o repositório não toca no histórico de eventos")
}

// TestIntegrationTransactionRepositoryRecategorize prova que o upsert
// atualiza a atribuição em vez de duplicar a transação.
func TestIntegrationTransactionRepositoryRecategorize(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewTransactionRepository(testClient)
	target, err := transaction.NewManual(
		mustUserID(t, uuid.New()),
		account.NewAccountID(),
		mustMoneyBRL(t, -20_00),
		time.Now().Add(-time.Hour),
		"Uber",
	)
	require.NoError(t, err)
	require.NoError(t, repository.Save(ctx, target))

	secondCategoryID := category.NewCategoryID()
	require.NoError(t, target.Categorize(category.NewCategoryID(), transaction.AssignedByUser))
	require.NoError(t, repository.Save(ctx, target))
	require.NoError(t, target.Categorize(secondCategoryID, transaction.AssignedByUser))
	require.NoError(t, repository.Save(ctx, target))

	found, err := repository.FindByID(ctx, target.ID())
	require.NoError(t, err)

	assignment, hasCategory := found.Category()
	require.True(t, hasCategory)
	foundCategoryID := assignment.CategoryID()
	assert.True(t, foundCategoryID.Equals(secondCategoryID))

	var rowCount int
	require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM transactions`).Scan(&rowCount))
	assert.Equal(t, 1, rowCount)
}

// TestIntegrationTransactionRepositoryNotFound: ausência → sentinel do
// aggregate.
func TestIntegrationTransactionRepositoryNotFound(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewTransactionRepository(testClient)
	missingID, err := transaction.TransactionIDFromUUID(uuid.New())
	require.NoError(t, err)

	found, err := repository.FindByID(ctx, missingID)

	require.ErrorIs(t, err, transaction.ErrNotFound)
	assert.Nil(t, found)
}

// TestIntegrationTransactionSchemaRejectsImpossibleState escreve SQL CRU pra
// provar que as CHECK constraints DECLARADAS NO SCHEMA DO ENT chegaram no
// banco. É o teste que garante que a migration gerada não perdeu invariante —
// se alguém escrever no banco por fora do domínio (script, correção manual),
// o schema ainda segura.
func TestIntegrationTransactionSchemaRejectsImpossibleState(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	const rawInsert = `
INSERT INTO transactions (
    id, user_id, account_id, amount, currency, occurred_at, description, reconciled,
    category_id, category_assigned_by, category_assigned_at,
    external_ref_provider, external_ref_provider_transaction_id,
    created_at, updated_at
) VALUES ($1, $2, $3, $4, 'BRL', now(), $5, $6, $7, $8, $9, $10, $11, now(), now())`

	cases := []struct {
		name                  string
		amount                int64
		description           string
		reconciled            bool
		categoryID            *uuid.UUID
		categoryAssignedBy    string
		categoryAssignedAt    *time.Time
		externalRefProvider   string
		externalRefProviderTx string
	}{
		{name: "quantia zero", amount: 0, description: "Nada"},
		{name: "descrição em branco", amount: -100, description: "   "},
		{name: "manual marcada como conciliada", amount: -100, description: "Uber", reconciled: true},
		{name: "categoria sem quem atribuiu", amount: -100, description: "Uber", categoryID: pointerTo(uuid.New())},
		{name: "referência externa sem id no provider", amount: -100, description: "Uber", externalRefProvider: "pluggy"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := testDB.ExecContext(ctx, rawInsert,
				uuid.New(), uuid.New(), uuid.New(),
				testCase.amount, testCase.description, testCase.reconciled,
				testCase.categoryID, testCase.categoryAssignedBy, testCase.categoryAssignedAt,
				testCase.externalRefProvider, testCase.externalRefProviderTx,
			)
			require.Error(t, err, "o schema tem que recusar este estado")
		})
	}
}

func pointerTo[T any](value T) *T {
	return &value
}

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
	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

// mustTransactionAt cria e persiste um lançamento com instante controlado.
func mustTransactionAt(t *testing.T, repository *entrepo.TransactionRepository, ownerID uuid.UUID, accountID account.AccountID, amount int64, occurredAt time.Time) *transaction.Transaction {
	t.Helper()

	created, err := transaction.NewManual(
		mustUserID(t, ownerID), accountID, mustMoneyBRL(t, amount), occurredAt, "Mercado")
	require.NoError(t, err)
	require.NoError(t, repository.Save(context.Background(), created))
	return created
}

// TestIntegrationAccountRepositoryListByUser cobre ordenação e isolamento por
// usuário direto no SQL — o filtro por dono é o que impede um extrato
// aparecer para outra pessoa.
func TestIntegrationAccountRepositoryListByUser(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewAccountRepository(testClient)
	ownerID := uuid.New()
	otherID := uuid.New()

	zebra, err := account.New(mustUserID(t, ownerID), "Zebra", account.KindChecking, mustCurrency(t, "BRL"), account.NewManualSource())
	require.NoError(t, err)
	alfa, err := account.New(mustUserID(t, ownerID), "Alfa", account.KindSavings, mustCurrency(t, "BRL"), account.NewManualSource())
	require.NoError(t, err)
	require.NoError(t, repository.Save(ctx, zebra))
	require.NoError(t, repository.Save(ctx, alfa))
	require.NoError(t, repository.Save(ctx, mustManualAccount(t, otherID)))

	found, err := repository.ListByUser(ctx, mustUserID(t, ownerID))

	require.NoError(t, err)
	require.Len(t, found, 2, "a conta do outro usuário não vem")
	assert.Equal(t, "Alfa", found[0].Name(), "ordenado por nome")
	assert.Equal(t, "Zebra", found[1].Name())
}

// TestIntegrationCategoryRepositoryListByUserCarregaRegras é o teste do N+1:
// as regras vêm no eager loading, não em uma consulta por categoria.
func TestIntegrationCategoryRepositoryListByUserCarregaRegras(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewCategoryRepository(testClient)
	ownerID := uuid.New()

	food, err := category.New(mustUserID(t, ownerID), "Alimentação", nil)
	require.NoError(t, err)
	require.NoError(t, food.AddRule(mustRule(t, "ifood")))
	require.NoError(t, food.AddRule(mustRule(t, "mercado")))
	require.NoError(t, repository.Save(ctx, food))

	transport, err := category.New(mustUserID(t, ownerID), "Transporte", nil)
	require.NoError(t, err)
	require.NoError(t, transport.AddRule(mustRule(t, "uber")))
	require.NoError(t, repository.Save(ctx, transport))

	require.NoError(t, repository.Save(ctx, mustCategoryFor(t, uuid.New())))

	found, err := repository.ListByUser(ctx, mustUserID(t, ownerID))

	require.NoError(t, err)
	require.Len(t, found, 2)
	assert.Equal(t, "Alimentação", found[0].Name())
	assert.Len(t, found[0].Rules(), 2, "as regras vêm carregadas junto")
	assert.Equal(t, "Transporte", found[1].Name())
	assert.Len(t, found[1].Rules(), 1)
}

// TestIntegrationTransactionRepositoryList cobre ordem, filtros e paginação
// no SQL de verdade.
func TestIntegrationTransactionRepositoryList(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewTransactionRepository(testClient)
	ownerID := uuid.New()
	accountID := account.NewAccountID()
	otherAccountID := account.NewAccountID()

	now := time.Now()
	oldest := mustTransactionAt(t, repository, ownerID, accountID, -100, now.Add(-72*time.Hour))
	middle := mustTransactionAt(t, repository, ownerID, accountID, -200, now.Add(-48*time.Hour))
	newest := mustTransactionAt(t, repository, ownerID, accountID, -300, now.Add(-24*time.Hour))
	mustTransactionAt(t, repository, ownerID, otherAccountID, -400, now.Add(-12*time.Hour))
	mustTransactionAt(t, repository, uuid.New(), accountID, -999, now.Add(-time.Hour))

	page, err := kernel.NewPage(kernel.DefaultLimit, 0)
	require.NoError(t, err)

	t.Run("ordem e isolamento por usuário", func(t *testing.T) {
		criteria, err := transaction.NewCriteria(mustUserID(t, ownerID), page)
		require.NoError(t, err)

		found, err := repository.List(ctx, criteria)

		require.NoError(t, err)
		require.Len(t, found, 4, "a transação do outro usuário não vem")
		assert.True(t, found[0].ID().Equals(newest.ID()) || found[0].OccurredAt().After(found[1].OccurredAt()),
			"do mais recente pro mais antigo")
		assert.True(t, found[3].ID().Equals(oldest.ID()))
	})

	t.Run("filtro por conta", func(t *testing.T) {
		criteria, err := transaction.NewCriteria(mustUserID(t, ownerID), page, transaction.ForAccount(otherAccountID))
		require.NoError(t, err)

		found, err := repository.List(ctx, criteria)

		require.NoError(t, err)
		require.Len(t, found, 1)
		assert.Equal(t, int64(-400), moneyAmount(found[0]))
	})

	t.Run("filtro por período", func(t *testing.T) {
		criteria, err := transaction.NewCriteria(mustUserID(t, ownerID), page,
			transaction.InPeriod(now.Add(-50*time.Hour), now.Add(-20*time.Hour)))
		require.NoError(t, err)

		found, err := repository.List(ctx, criteria)

		require.NoError(t, err)
		require.Len(t, found, 2, "só middle e newest caem na janela")
		assert.True(t, found[1].ID().Equals(middle.ID()))
	})

	t.Run("paginação não repete nem perde registro", func(t *testing.T) {
		firstPage, err := kernel.NewPage(2, 0)
		require.NoError(t, err)
		secondPage, err := kernel.NewPage(2, 2)
		require.NoError(t, err)

		firstCriteria, err := transaction.NewCriteria(mustUserID(t, ownerID), firstPage)
		require.NoError(t, err)
		secondCriteria, err := transaction.NewCriteria(mustUserID(t, ownerID), secondPage)
		require.NoError(t, err)

		first, err := repository.List(ctx, firstCriteria)
		require.NoError(t, err)
		second, err := repository.List(ctx, secondCriteria)
		require.NoError(t, err)

		require.Len(t, first, 2)
		require.Len(t, second, 2)

		seen := map[string]bool{}
		for _, current := range append(first, second...) {
			id := current.ID().String()
			assert.False(t, seen[id], "id repetido entre páginas: %s", id)
			seen[id] = true
		}
		assert.Len(t, seen, 4)
	})

	t.Run("filtro por categoria", func(t *testing.T) {
		categoryID := category.NewCategoryID()
		require.NoError(t, middle.Categorize(categoryID, transaction.AssignedByUser))
		require.NoError(t, repository.Save(ctx, middle))

		criteria, err := transaction.NewCriteria(mustUserID(t, ownerID), page, transaction.ForCategory(categoryID))
		require.NoError(t, err)

		found, err := repository.List(ctx, criteria)

		require.NoError(t, err)
		require.Len(t, found, 1)
		assert.True(t, found[0].ID().Equals(middle.ID()))
	})

	t.Run("nada encontrado é lista vazia, não erro", func(t *testing.T) {
		criteria, err := transaction.NewCriteria(mustUserID(t, uuid.New()), page)
		require.NoError(t, err)

		found, err := repository.List(ctx, criteria)

		require.NoError(t, err)
		assert.Empty(t, found)
	})
}

// moneyAmount encurta a leitura do valor nos testes.
func moneyAmount(current *transaction.Transaction) int64 {
	money := current.Money()
	return money.Amount()
}

// mustCategoryFor cria e devolve uma categoria simples de outro usuário.
func mustCategoryFor(t *testing.T, ownerID uuid.UUID) *category.Category {
	t.Helper()
	created, err := category.New(mustUserID(t, ownerID), "Alheia", nil)
	require.NoError(t, err)
	return created
}

package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

// TestListAccounts cobre a projeção e o ISOLAMENTO por usuário — que é o
// requisito de segurança das listagens: sem filtro por dono, o extrato de um
// usuário apareceria para outro.
func TestListAccounts(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	otherID := uuid.New()

	manual := newManualAccount(t, ownerID, "BRL")
	connected := newConnectedAccount(t, ownerID, "pluggy", "pluggy-acc-1", "BRL")
	fromOther := newManualAccount(t, otherID, "BRL")

	accounts := newFakeAccounts(manual, connected, fromOther)
	useCase := application.NewListAccountsUseCase(accounts)

	output, err := useCase.Execute(context.Background(), application.ListAccountsInput{UserID: ownerID})

	require.NoError(t, err)
	require.Len(t, output.Accounts, 2, "a conta do outro usuário não aparece")

	// Ordenado por nome: "Conta Conectada" antes de "Conta Corrente".
	assert.Equal(t, "Conta Conectada", output.Accounts[0].Name)
	assert.Equal(t, "pluggy", output.Accounts[0].Provider, "conta conectada informa o provedor")
	assert.Equal(t, "Conta Corrente", output.Accounts[1].Name)
	assert.Empty(t, output.Accounts[1].Provider, "conta manual não tem provedor")
	assert.False(t, output.Accounts[0].BalanceAsOf.IsZero(), "saldo sem data mente")
}

// TestViewAccount cobre detalhe e as duas recusas do id vindo do cliente.
func TestViewAccount(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	target := newManualAccount(t, ownerID, "BRL")
	accountID := target.ID()
	useCase := application.NewViewAccountUseCase(newFakeAccounts(target))

	t.Run("caminho feliz", func(t *testing.T) {
		t.Parallel()

		summary, err := useCase.Execute(context.Background(), application.ViewAccountInput{
			UserID: ownerID, AccountID: accountID.UUID(),
		})

		require.NoError(t, err)
		assert.Equal(t, accountID.String(), summary.ID)
		assert.Equal(t, "Conta Corrente", summary.Name)
	})

	t.Run("conta de outro usuário", func(t *testing.T) {
		t.Parallel()

		_, err := useCase.Execute(context.Background(), application.ViewAccountInput{
			UserID: uuid.New(), AccountID: accountID.UUID(),
		})

		require.ErrorIs(t, err, shared.ErrForbidden)
	})

	t.Run("conta inexistente", func(t *testing.T) {
		t.Parallel()

		_, err := useCase.Execute(context.Background(), application.ViewAccountInput{
			UserID: ownerID, AccountID: uuid.New(),
		})

		require.ErrorIs(t, err, account.ErrNotFound)
	})
}

// TestListCategories cobre a projeção com regras dentro.
func TestListCategories(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	food := newCategory(t, ownerID, "Alimentação")
	rule, err := category.NewCategoryRule("ifood")
	require.NoError(t, err)
	require.NoError(t, food.AddRule(rule))

	parentID := food.ID()
	restaurants, err := category.New(mustUserID(t, ownerID), "Restaurantes", &parentID)
	require.NoError(t, err)

	fromOther := newCategory(t, uuid.New(), "Alheia")

	useCase := application.NewListCategoriesUseCase(newFakeCategories(food, restaurants, fromOther))

	output, err := useCase.Execute(context.Background(), application.ListCategoriesInput{UserID: ownerID})

	require.NoError(t, err)
	require.Len(t, output.Categories, 2)

	assert.Equal(t, "Alimentação", output.Categories[0].Name)
	assert.Nil(t, output.Categories[0].ParentID)
	require.Len(t, output.Categories[0].Rules, 1, "as regras vêm junto: estão dentro do aggregate")
	assert.Equal(t, "ifood", output.Categories[0].Rules[0].Keyword)

	require.NotNil(t, output.Categories[1].ParentID)
	assert.Equal(t, parentID.String(), *output.Categories[1].ParentID)
}

// TestListTransactions cobre projeção, ordem e paginação.
func TestListTransactions(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	targetAccount := newManualAccount(t, ownerID, "BRL")
	accountID := targetAccount.ID()

	older := newManualTransactionAt(t, ownerID, accountID, -100, time.Now().Add(-72*time.Hour))
	middle := newManualTransactionAt(t, ownerID, accountID, -200, time.Now().Add(-48*time.Hour))
	newest := newManualTransactionAt(t, ownerID, accountID, -300, time.Now().Add(-24*time.Hour))
	fromOther := newManualTransactionAt(t, uuid.New(), account.NewAccountID(), -999, time.Now())

	transactions := newFakeTransactions(older, middle, newest, fromOther)
	useCase := application.NewListTransactionsUseCase(transactions)

	t.Run("ordem e isolamento", func(t *testing.T) {
		t.Parallel()

		output, err := useCase.Execute(context.Background(), application.ListTransactionsInput{UserID: ownerID})

		require.NoError(t, err)
		require.Len(t, output.Transactions, 3, "transação de outro usuário não aparece")
		assert.Equal(t, int64(-300), output.Transactions[0].Amount, "extrato começa pela mais recente")
		assert.Equal(t, int64(-100), output.Transactions[2].Amount)
		assert.Equal(t, kernel.DefaultLimit, output.Limit, "o default é devolvido pra o cliente montar a próxima página")
	})

	t.Run("paginação", func(t *testing.T) {
		t.Parallel()

		first, err := useCase.Execute(context.Background(), application.ListTransactionsInput{
			UserID: ownerID, Limit: 2, Offset: 0,
		})
		require.NoError(t, err)
		require.Len(t, first.Transactions, 2)

		second, err := useCase.Execute(context.Background(), application.ListTransactionsInput{
			UserID: ownerID, Limit: 2, Offset: 2,
		})
		require.NoError(t, err)
		require.Len(t, second.Transactions, 1)
		assert.NotEqual(t, first.Transactions[0].ID, second.Transactions[0].ID)
	})

	t.Run("filtro por período", func(t *testing.T) {
		t.Parallel()

		output, err := useCase.Execute(context.Background(), application.ListTransactionsInput{
			UserID: ownerID,
			From:   time.Now().Add(-50 * time.Hour),
			To:     time.Now(),
		})

		require.NoError(t, err)
		assert.Len(t, output.Transactions, 2, "a de 72h atrás fica fora")
	})

	t.Run("filtro por conta", func(t *testing.T) {
		t.Parallel()

		otherAccountID := account.NewAccountID()
		rawOther := otherAccountID.UUID()
		output, err := useCase.Execute(context.Background(), application.ListTransactionsInput{
			UserID: ownerID, AccountID: &rawOther,
		})

		require.NoError(t, err)
		assert.Empty(t, output.Transactions)
	})
}

// TestListTransactionsRecusa: filtro inválido morre na construção do
// critério, antes de virar consulta.
func TestListTransactionsRecusa(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	nilUUID := uuid.Nil

	cases := []struct {
		name    string
		input   application.ListTransactionsInput
		wantErr error
	}{
		{
			name:    "usuário nil",
			input:   application.ListTransactionsInput{UserID: uuid.Nil},
			wantErr: shared.ErrInvalidUserID,
		},
		{
			name:    "limite acima do teto",
			input:   application.ListTransactionsInput{UserID: ownerID, Limit: kernel.MaxLimit + 1},
			wantErr: kernel.ErrInvalidPage,
		},
		{
			name:    "offset negativo",
			input:   application.ListTransactionsInput{UserID: ownerID, Offset: -1},
			wantErr: kernel.ErrInvalidPage,
		},
		{
			name:    "conta nil no filtro",
			input:   application.ListTransactionsInput{UserID: ownerID, AccountID: &nilUUID},
			wantErr: account.ErrInvalidID,
		},
		{
			name:    "categoria nil no filtro",
			input:   application.ListTransactionsInput{UserID: ownerID, CategoryID: &nilUUID},
			wantErr: category.ErrInvalidID,
		},
		{
			name: "período invertido",
			input: application.ListTransactionsInput{
				UserID: ownerID,
				From:   time.Now(),
				To:     time.Now().Add(-24 * time.Hour),
			},
			wantErr: transaction.ErrInvalidPeriod,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			useCase := application.NewListTransactionsUseCase(newFakeTransactions())

			_, err := useCase.Execute(context.Background(), testCase.input)

			require.ErrorIs(t, err, testCase.wantErr)
		})
	}
}

// TestViewTransaction cobre o detalhe, incluindo o bloco de categoria.
func TestViewTransaction(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	targetAccount := newManualAccount(t, ownerID, "BRL")
	accountID := targetAccount.ID()
	target := newManualTransaction(t, ownerID, accountID, -4550, "BRL")
	categoryID := category.NewCategoryID()
	require.NoError(t, target.Categorize(categoryID, transaction.AssignedByUser))
	transactionID := target.ID()

	useCase := application.NewViewTransactionUseCase(newFakeTransactions(target))

	t.Run("caminho feliz com categoria", func(t *testing.T) {
		t.Parallel()

		summary, err := useCase.Execute(context.Background(), application.ViewTransactionInput{
			UserID: ownerID, TransactionID: transactionID.UUID(),
		})

		require.NoError(t, err)
		assert.Equal(t, int64(-4550), summary.Amount)
		require.NotNil(t, summary.CategoryID)
		assert.Equal(t, categoryID.String(), *summary.CategoryID)
		require.NotNil(t, summary.AssignedBy)
		assert.Equal(t, "user", *summary.AssignedBy)
		require.NotNil(t, summary.AssignedAt)
		assert.Empty(t, summary.Provider, "lançamento manual não tem provedor")
	})

	t.Run("transação de outro usuário", func(t *testing.T) {
		t.Parallel()

		_, err := useCase.Execute(context.Background(), application.ViewTransactionInput{
			UserID: uuid.New(), TransactionID: transactionID.UUID(),
		})

		require.ErrorIs(t, err, shared.ErrForbidden)
	})
}

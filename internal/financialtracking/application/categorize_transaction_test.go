package application_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

// TestCategorizeTransactionUseCase_Execute cobre o caminho feliz e a ordem
// Save → Dispatch → ClearEvents: o evento sai DEPOIS de persistir, e o
// aggregate fica com o histórico limpo.
func TestCategorizeTransactionUseCase_Execute(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	targetAccount := newManualAccount(t, ownerID, "BRL")
	accountID := targetAccount.ID()
	target := newManualTransaction(t, ownerID, accountID, -3200, "BRL")
	targetCategory := newCategory(t, ownerID, "Alimentação")

	transactions := newFakeTransactions(target)
	categories := newFakeCategories(targetCategory)
	dispatcher := &fakeDispatcher{}
	useCase := application.NewCategorizeTransactionUseCase(transactions, categories, dispatcher)

	transactionID := target.ID()
	categoryID := targetCategory.ID()
	output, err := useCase.Execute(context.Background(), application.CategorizeTransactionInput{
		UserID:        ownerID,
		TransactionID: transactionID.UUID(),
		CategoryID:    categoryID.UUID(),
		AssignedBy:    "user",
	})

	require.NoError(t, err)
	assert.Equal(t, transactionID.String(), output.TransactionID)
	assert.Equal(t, categoryID.String(), output.CategoryID)
	assert.Equal(t, "user", output.AssignedBy)
	assert.False(t, output.AssignedAt.IsZero())

	require.Len(t, transactions.saved, 1, "a transação categorizada tem que ser persistida")
	assert.Equal(t, []string{transaction.EventTypeCategorized}, dispatcher.eventNames())
	assert.Empty(t, target.Events(), "ClearEvents roda depois do dispatch")
	assert.Empty(t, categories.saved, "a categoria é só LIDA neste use case")
}

// TestCategorizeTransactionUseCase_Execute_Recategoriza cobre a decisão de
// domínio de 2026-07-07: trocar a categoria SUBSTITUI a atribuição e
// RE-EMITE o evento (Budgets precisa saber da troca).
func TestCategorizeTransactionUseCase_Execute_Recategoriza(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	targetAccount := newManualAccount(t, ownerID, "BRL")
	accountID := targetAccount.ID()
	target := newManualTransaction(t, ownerID, accountID, -3200, "BRL")
	first := newCategory(t, ownerID, "Alimentação")
	second := newCategory(t, ownerID, "Restaurantes")

	transactions := newFakeTransactions(target)
	categories := newFakeCategories(first, second)
	dispatcher := &fakeDispatcher{}
	useCase := application.NewCategorizeTransactionUseCase(transactions, categories, dispatcher)

	transactionID := target.ID()
	firstID := first.ID()
	secondID := second.ID()

	_, err := useCase.Execute(context.Background(), application.CategorizeTransactionInput{
		UserID: ownerID, TransactionID: transactionID.UUID(), CategoryID: firstID.UUID(), AssignedBy: "user",
	})
	require.NoError(t, err)

	output, err := useCase.Execute(context.Background(), application.CategorizeTransactionInput{
		UserID: ownerID, TransactionID: transactionID.UUID(), CategoryID: secondID.UUID(), AssignedBy: "rule",
	})
	require.NoError(t, err)

	assert.Equal(t, secondID.String(), output.CategoryID)
	assert.Equal(t, "rule", output.AssignedBy)
	assert.Len(t, dispatcher.eventNames(), 2, "recategorizar re-emite o evento")
}

// TestCategorizeTransactionUseCase_Execute_Autorizacao cobre os dois donos
// que precisam bater: o da transação e o da categoria. Categoria alheia
// morre em ErrForbidden — responder ErrNotFound vazaria a existência dela.
func TestCategorizeTransactionUseCase_Execute_Autorizacao(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		transactionOwner uuid.UUID
		categoryOwner    uuid.UUID
	}{
		{name: "transação de outro usuário", transactionOwner: uuid.New(), categoryOwner: uuid.Nil},
		{name: "categoria de outro usuário", transactionOwner: uuid.Nil, categoryOwner: uuid.New()},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requesterID := uuid.New()
			transactionOwner := testCase.transactionOwner
			if transactionOwner == uuid.Nil {
				transactionOwner = requesterID
			}
			categoryOwner := testCase.categoryOwner
			if categoryOwner == uuid.Nil {
				categoryOwner = requesterID
			}

			targetAccount := newManualAccount(t, transactionOwner, "BRL")
			accountID := targetAccount.ID()
			target := newManualTransaction(t, transactionOwner, accountID, -1000, "BRL")
			targetCategory := newCategory(t, categoryOwner, "Alimentação")

			transactions := newFakeTransactions(target)
			categories := newFakeCategories(targetCategory)
			dispatcher := &fakeDispatcher{}
			useCase := application.NewCategorizeTransactionUseCase(transactions, categories, dispatcher)

			transactionID := target.ID()
			categoryID := targetCategory.ID()
			_, err := useCase.Execute(context.Background(), application.CategorizeTransactionInput{
				UserID:        requesterID,
				TransactionID: transactionID.UUID(),
				CategoryID:    categoryID.UUID(),
				AssignedBy:    "user",
			})

			require.ErrorIs(t, err, shared.ErrForbidden)
			assert.Empty(t, transactions.saved)
			assert.Empty(t, dispatcher.dispatched)
		})
	}
}

// TestCategorizeTransactionUseCase_Execute_Recusa cobre inputs inválidos e
// aggregates inexistentes.
func TestCategorizeTransactionUseCase_Execute_Recusa(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		mutateInput func(input *application.CategorizeTransactionInput)
		wantErr     error
	}{
		{
			name:        "usuário nil",
			mutateInput: func(input *application.CategorizeTransactionInput) { input.UserID = uuid.Nil },
			wantErr:     shared.ErrInvalidUserID,
		},
		{
			name:        "transação nil",
			mutateInput: func(input *application.CategorizeTransactionInput) { input.TransactionID = uuid.Nil },
			wantErr:     transaction.ErrInvalidID,
		},
		{
			name:        "categoria nil",
			mutateInput: func(input *application.CategorizeTransactionInput) { input.CategoryID = uuid.Nil },
			wantErr:     category.ErrInvalidID,
		},
		{
			name:        "assignedBy fora do vocabulário",
			mutateInput: func(input *application.CategorizeTransactionInput) { input.AssignedBy = "robot" },
			wantErr:     transaction.ErrInvalidAssignment,
		},
		{
			name:        "transação inexistente",
			mutateInput: func(input *application.CategorizeTransactionInput) { input.TransactionID = uuid.New() },
			wantErr:     transaction.ErrNotFound,
		},
		{
			name:        "categoria inexistente",
			mutateInput: func(input *application.CategorizeTransactionInput) { input.CategoryID = uuid.New() },
			wantErr:     category.ErrNotFound,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ownerID := uuid.New()
			targetAccount := newManualAccount(t, ownerID, "BRL")
			accountID := targetAccount.ID()
			target := newManualTransaction(t, ownerID, accountID, -1000, "BRL")
			targetCategory := newCategory(t, ownerID, "Alimentação")

			transactions := newFakeTransactions(target)
			categories := newFakeCategories(targetCategory)
			dispatcher := &fakeDispatcher{}
			useCase := application.NewCategorizeTransactionUseCase(transactions, categories, dispatcher)

			transactionID := target.ID()
			categoryID := targetCategory.ID()
			input := application.CategorizeTransactionInput{
				UserID:        ownerID,
				TransactionID: transactionID.UUID(),
				CategoryID:    categoryID.UUID(),
				AssignedBy:    "user",
			}
			testCase.mutateInput(&input)

			_, err := useCase.Execute(context.Background(), input)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, transactions.saved)
			assert.Empty(t, dispatcher.dispatched)
		})
	}
}

// TestCategorizeTransactionUseCase_Execute_DispatchFalhando cobre a regra do
// v1: dispatcher síncrono, erro de handler aborta o use case. E o aggregate
// NÃO perde os eventos — ClearEvents só roda se o dispatch deu certo.
func TestCategorizeTransactionUseCase_Execute_DispatchFalhando(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	targetAccount := newManualAccount(t, ownerID, "BRL")
	accountID := targetAccount.ID()
	target := newManualTransaction(t, ownerID, accountID, -1000, "BRL")
	targetCategory := newCategory(t, ownerID, "Alimentação")

	transactions := newFakeTransactions(target)
	categories := newFakeCategories(targetCategory)
	dispatcher := &fakeDispatcher{dispatchErr: errInfra}
	useCase := application.NewCategorizeTransactionUseCase(transactions, categories, dispatcher)

	transactionID := target.ID()
	categoryID := targetCategory.ID()
	_, err := useCase.Execute(context.Background(), application.CategorizeTransactionInput{
		UserID:        ownerID,
		TransactionID: transactionID.UUID(),
		CategoryID:    categoryID.UUID(),
		AssignedBy:    "user",
	})

	require.ErrorIs(t, err, errInfra)
	require.Len(t, transactions.saved, 1, "o Save já tinha acontecido — o v1 não tem transação de banco aqui")
	assert.Len(t, target.Events(), 1, "evento não despachado fica no aggregate")
}

// TestCategorizeTransactionUseCase_Execute_PropagaErroDeInfra cobre as três
// fronteiras de persistência deste use case.
func TestCategorizeTransactionUseCase_Execute_PropagaErroDeInfra(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		arrange func(transactions *fakeTransactions, categories *fakeCategories)
	}{
		{
			name:    "falha lendo a transação",
			arrange: func(transactions *fakeTransactions, _ *fakeCategories) { transactions.findErr = errInfra },
		},
		{
			name:    "falha lendo a categoria",
			arrange: func(_ *fakeTransactions, categories *fakeCategories) { categories.findErr = errInfra },
		},
		{
			name:    "falha gravando a transação",
			arrange: func(transactions *fakeTransactions, _ *fakeCategories) { transactions.saveErr = errInfra },
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ownerID := uuid.New()
			targetAccount := newManualAccount(t, ownerID, "BRL")
			accountID := targetAccount.ID()
			target := newManualTransaction(t, ownerID, accountID, -1000, "BRL")
			targetCategory := newCategory(t, ownerID, "Alimentação")

			transactions := newFakeTransactions(target)
			categories := newFakeCategories(targetCategory)
			testCase.arrange(transactions, categories)

			dispatcher := &fakeDispatcher{}
			useCase := application.NewCategorizeTransactionUseCase(transactions, categories, dispatcher)

			transactionID := target.ID()
			categoryID := targetCategory.ID()
			_, err := useCase.Execute(context.Background(), application.CategorizeTransactionInput{
				UserID:        ownerID,
				TransactionID: transactionID.UUID(),
				CategoryID:    categoryID.UUID(),
				AssignedBy:    "user",
			})

			require.ErrorIs(t, err, errInfra)
			assert.Empty(t, dispatcher.dispatched, "evento não sai se a persistência falhou")
		})
	}
}

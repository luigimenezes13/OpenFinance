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
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

// TestRecordTransactionUseCase_Execute cobre o caminho feliz de uma SAÍDA
// (valor negativo na convenção patrimônio) e a decisão de a transação
// HERDAR a moeda da conta.
func TestRecordTransactionUseCase_Execute(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	targetAccount := newManualAccount(t, ownerID, "BRL")
	accountID := targetAccount.ID()

	accounts := newFakeAccounts(targetAccount)
	transactions := newFakeTransactions()
	useCase := application.NewRecordTransactionUseCase(transactions, accounts)

	occurredAt := time.Now().Add(-2 * time.Hour)
	output, err := useCase.Execute(context.Background(), application.RecordTransactionInput{
		UserID:      ownerID,
		AccountID:   accountID.UUID(),
		Amount:      -4550,
		OccurredAt:  occurredAt,
		Description: "  Padaria  ",
	})

	require.NoError(t, err)
	assert.Equal(t, int64(-4550), output.Amount)
	assert.Equal(t, "BRL", output.Currency, "a moeda vem da conta, não do input")
	assert.Equal(t, "Padaria", output.Description)
	assert.Equal(t, accountID.String(), output.AccountID)
	assert.WithinDuration(t, occurredAt, output.OccurredAt, time.Second)

	require.Len(t, transactions.saved, 1)
	saved := transactions.saved[0]
	assert.Empty(t, saved.Events(), "lançamento manual não emite evento — só o fluxo de provider emite")

	savedBalance := targetAccount.Balance()
	savedMoney := savedBalance.Money()
	assert.Zero(t, savedMoney.Amount(), "o v1 não mexe no saldo ao lançar (decisão do spec)")
	assert.Empty(t, accounts.saved, "a conta é só LIDA neste use case")
}

// TestRecordTransactionUseCase_Execute_RecusaContaDeOutroUsuario é o teste
// de autorização: dono errado morre em ErrForbidden ANTES de qualquer
// escrita.
func TestRecordTransactionUseCase_Execute_RecusaContaDeOutroUsuario(t *testing.T) {
	t.Parallel()

	otherAccount := newManualAccount(t, uuid.New(), "BRL")
	accounts := newFakeAccounts(otherAccount)
	transactions := newFakeTransactions()
	useCase := application.NewRecordTransactionUseCase(transactions, accounts)

	accountID := otherAccount.ID()
	_, err := useCase.Execute(context.Background(), application.RecordTransactionInput{
		UserID:      uuid.New(), // outro usuário
		AccountID:   accountID.UUID(),
		Amount:      1000,
		OccurredAt:  time.Now().Add(-time.Minute),
		Description: "Tentativa",
	})

	require.ErrorIs(t, err, shared.ErrForbidden)
	assert.Empty(t, transactions.saved)
}

// TestRecordTransactionUseCase_Execute_Recusa cobre os inputs inválidos e a
// conta inexistente.
func TestRecordTransactionUseCase_Execute_Recusa(t *testing.T) {
	t.Parallel()

	validOccurredAt := time.Now().Add(-time.Hour)

	cases := []struct {
		name        string
		mutateInput func(input *application.RecordTransactionInput)
		wantErr     error
	}{
		{
			name:        "usuário nil",
			mutateInput: func(input *application.RecordTransactionInput) { input.UserID = uuid.Nil },
			wantErr:     shared.ErrInvalidUserID,
		},
		{
			name:        "conta nil",
			mutateInput: func(input *application.RecordTransactionInput) { input.AccountID = uuid.Nil },
			wantErr:     account.ErrInvalidID,
		},
		{
			name:        "conta inexistente",
			mutateInput: func(input *application.RecordTransactionInput) { input.AccountID = uuid.New() },
			wantErr:     account.ErrNotFound,
		},
		{
			name:        "valor zero",
			mutateInput: func(input *application.RecordTransactionInput) { input.Amount = 0 },
			wantErr:     transaction.ErrZeroMoney,
		},
		{
			name:        "descrição vazia",
			mutateInput: func(input *application.RecordTransactionInput) { input.Description = "  " },
			wantErr:     transaction.ErrInvalidDescription,
		},
		{
			name:        "data no futuro",
			mutateInput: func(input *application.RecordTransactionInput) { input.OccurredAt = time.Now().Add(time.Hour) },
			wantErr:     transaction.ErrInvalidOccurredAt,
		},
		{
			name:        "data zero",
			mutateInput: func(input *application.RecordTransactionInput) { input.OccurredAt = time.Time{} },
			wantErr:     transaction.ErrInvalidOccurredAt,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ownerID := uuid.New()
			targetAccount := newManualAccount(t, ownerID, "BRL")
			accountID := targetAccount.ID()
			accounts := newFakeAccounts(targetAccount)
			transactions := newFakeTransactions()
			useCase := application.NewRecordTransactionUseCase(transactions, accounts)

			input := application.RecordTransactionInput{
				UserID:      ownerID,
				AccountID:   accountID.UUID(),
				Amount:      -1000,
				OccurredAt:  validOccurredAt,
				Description: "Mercado",
			}
			testCase.mutateInput(&input)

			_, err := useCase.Execute(context.Background(), input)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, transactions.saved)
		})
	}
}

// TestRecordTransactionUseCase_Execute_PropagaErroDeInfra cobre as duas
// fronteiras: falha ao LER a conta e falha ao GRAVAR a transação.
func TestRecordTransactionUseCase_Execute_PropagaErroDeInfra(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		arrange func(accounts *fakeAccounts, transactions *fakeTransactions)
	}{
		{
			name:    "falha lendo a conta",
			arrange: func(accounts *fakeAccounts, _ *fakeTransactions) { accounts.findErr = errInfra },
		},
		{
			name:    "falha gravando a transação",
			arrange: func(_ *fakeAccounts, transactions *fakeTransactions) { transactions.saveErr = errInfra },
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ownerID := uuid.New()
			targetAccount := newManualAccount(t, ownerID, "BRL")
			accountID := targetAccount.ID()
			accounts := newFakeAccounts(targetAccount)
			transactions := newFakeTransactions()
			testCase.arrange(accounts, transactions)

			useCase := application.NewRecordTransactionUseCase(transactions, accounts)
			_, err := useCase.Execute(context.Background(), application.RecordTransactionInput{
				UserID:      ownerID,
				AccountID:   accountID.UUID(),
				Amount:      2500,
				OccurredAt:  time.Now().Add(-time.Hour),
				Description: "Salário",
			})

			require.ErrorIs(t, err, errInfra)
		})
	}
}

// TestRecordTransactionNuncaMexeNoSaldo é TESTE DE REGRESSÃO de uma regra de
// produto, não de um detalhe de implementação: o saldo entra no sistema por
// um caminho só, a importação Open Finance (firmado em 2026-08-27).
//
// O banco é a autoridade sobre quanto existe na conta. Se um dia alguém
// "consertar" isto somando os lançamentos no saldo, o número passa a
// discordar do extrato bancário — e o usuário confia no errado. É esse
// conserto bem-intencionado que este teste existe pra impedir.
func TestRecordTransactionNuncaMexeNoSaldo(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	targetAccount := newManualAccount(t, ownerID, "BRL")
	accountID := targetAccount.ID()

	// Parte de um saldo NÃO-ZERO, como se tivesse vindo de uma importação:
	// com saldo zero, um teste passaria mesmo se o código somasse errado e
	// resultasse em zero por coincidência.
	// asOf DEPOIS do instante em que a conta nasceu: o aggregate recusa
	// saldo que volta no tempo (ErrStaleBalance), então um asOf no passado
	// aqui faria o setup do próprio teste falhar.
	initial, err := account.NewBalance(mustMoney(t, 1_000_00, "BRL"), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.NoError(t, targetAccount.UpdateBalance(initial))
	targetAccount.ClearEvents()

	accounts := newFakeAccounts(targetAccount)
	transactions := newFakeTransactions()
	useCase := application.NewRecordTransactionUseCase(transactions, accounts)

	// Vários lançamentos, entrada e saída.
	for _, amount := range []int64{-45_50, -120_00, 300_00} {
		_, err := useCase.Execute(context.Background(), application.RecordTransactionInput{
			UserID:      ownerID,
			AccountID:   accountID.UUID(),
			Amount:      amount,
			OccurredAt:  time.Now().Add(-time.Minute),
			Description: "Lançamento",
		})
		require.NoError(t, err)
	}

	require.Len(t, transactions.saved, 3, "as transações entraram")

	balance := targetAccount.Balance()
	money := balance.Money()
	assert.Equal(t, int64(1_000_00), money.Amount(), "o saldo continua exatamente o que o provedor informou")
	assert.Empty(t, accounts.saved, "a conta nem é regravada: este use case só LÊ a conta")
	assert.Empty(t, targetAccount.Events(), "nenhum AccountBalanceUpdated foi emitido")
}

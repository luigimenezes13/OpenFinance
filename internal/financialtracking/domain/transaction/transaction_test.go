package transaction_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helpers compartilhados pelos _test.go deste package. Testes dos VOs
// (ExternalRef, AssignedBy, CategoryAssignment) ficam por sua conta em
// externalref_test.go e categoryassignment_test.go.

const validUUID = "550e8400-e29b-41d4-a716-446655440000"

func mustUserID(t *testing.T, raw string) shared.UserID {
	t.Helper()
	userID, err := shared.NewUserID(uuid.MustParse(raw))
	require.NoError(t, err)
	return userID
}

func mustMoney(t *testing.T, amount int64, code string) shared.Money {
	t.Helper()
	currency, err := shared.NewCurrency(code)
	require.NoError(t, err)
	money, err := shared.NewMoney(amount, currency)
	require.NoError(t, err)
	return money
}

func mustExternalRef(t *testing.T) transaction.ExternalRef {
	t.Helper()
	ref, err := transaction.NewExternalRef("pluggy", "tx-abc-123")
	require.NoError(t, err)
	return ref
}

// newValidManualTransaction cria um lançamento manual válido: R$ -45,50
// (saída, convenção patrimônio).
func newValidManualTransaction(t *testing.T) *transaction.Transaction {
	t.Helper()
	manualTransaction, err := transaction.NewManual(mustUserID(t, validUUID), account.NewAccountID(), mustMoney(t, -45_50, "BRL"), time.Now(), "Mercado")
	require.NoError(t, err)
	return manualTransaction
}

// TestNewManual — invariantes do lançamento manual.
func TestNewManual(t *testing.T) {
	t.Parallel()

	validUser := mustUserID(t, validUUID)
	validAccountID := account.NewAccountID()
	expense := mustMoney(t, -45_50, "BRL")
	now := time.Now()

	cases := []struct {
		name        string
		userID      shared.UserID
		accountID   account.AccountID
		money       shared.Money
		occurredAt  time.Time
		description string
		wantErr     error
	}{
		{"happy path", validUser, validAccountID, expense, now, "Mercado", nil},
		{"userID zero value", shared.UserID{}, validAccountID, expense, now, "Mercado", shared.ErrInvalidUserID},
		{"accountID zero value", validUser, account.AccountID{}, expense, now, "Mercado", account.ErrInvalidID},
		{"Money zero value", validUser, validAccountID, shared.Money{}, now, "Mercado", transaction.ErrInvalidMoney},
		{"quantia zero não é transação", validUser, validAccountID, mustMoney(t, 0, "BRL"), now, "Mercado", transaction.ErrZeroMoney},
		{"occurredAt zero", validUser, validAccountID, expense, time.Time{}, "Mercado", transaction.ErrInvalidOccurredAt},
		{"occurredAt no futuro é recusado", validUser, validAccountID, expense, now.Add(time.Hour), "Mercado", transaction.ErrInvalidOccurredAt},
		{"descrição vazia", validUser, validAccountID, expense, now, "", transaction.ErrInvalidDescription},
		{"descrição só com espaços", validUser, validAccountID, expense, now, "   ", transaction.ErrInvalidDescription},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := transaction.NewManual(tc.userID, tc.accountID, tc.money, tc.occurredAt, tc.description)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.False(t, got.ID().IsZero(), "NewManual deve gerar identidade")
			assert.True(t, got.Money().Equals(tc.money))
			assert.Equal(t, tc.description, got.Description())
			assert.False(t, got.IsReconciled())

			_, categorized := got.Category()
			assert.False(t, categorized, "nasce sem categoria")
			_, imported := got.ExternalRef()
			assert.False(t, imported, "manual não tem referência externa")

			assert.Empty(t, got.Events(), "lançamento manual não emite evento")
		})
	}

	t.Run("descrição é normalizada (trim)", func(t *testing.T) {
		t.Parallel()

		got, err := transaction.NewManual(validUser, validAccountID, expense, now, "  Mercado  ")

		require.NoError(t, err)
		assert.Equal(t, "Mercado", got.Description())
	})
}

// TestNewFromProvider — o caminho de import emite Imported.
func TestNewFromProvider(t *testing.T) {
	t.Parallel()

	t.Run("cria a transação e emite Imported", func(t *testing.T) {
		t.Parallel()

		ref := mustExternalRef(t)

		got, err := transaction.NewFromProvider(mustUserID(t, validUUID), account.NewAccountID(), mustMoney(t, -99_90, "BRL"), time.Now(), "Uber", ref)

		require.NoError(t, err)

		gotRef, ok := got.ExternalRef()
		require.True(t, ok, "transação de provider carrega a referência")
		assert.Equal(t, "pluggy", gotRef.Provider())

		raised := got.Events()
		require.Len(t, raised, 1)
		event, ok := raised[0].(transaction.Imported)
		require.True(t, ok, "evento deve ser transaction.Imported")
		assert.Equal(t, transaction.EventTypeImported, event.EventName())
		assert.True(t, event.TransactionID().Equals(got.ID()))
		assert.Equal(t, "tx-abc-123", event.Ref().ProviderTransactionID())
	})

	t.Run("ref zero value é recusada", func(t *testing.T) {
		t.Parallel()

		_, err := transaction.NewFromProvider(mustUserID(t, validUUID), account.NewAccountID(), mustMoney(t, -99_90, "BRL"), time.Now(), "Uber", transaction.ExternalRef{})

		require.ErrorIs(t, err, transaction.ErrInvalidRef)
	})
}

// TestCategorize — atribuição de categoria + evento.
func TestCategorize(t *testing.T) {
	t.Parallel()

	t.Run("categoriza e emite Categorized", func(t *testing.T) {
		t.Parallel()

		manualTransaction := newValidManualTransaction(t)
		categoryID := category.NewCategoryID()

		require.NoError(t, manualTransaction.Categorize(categoryID, transaction.AssignedByUser))

		assignment, ok := manualTransaction.Category()
		require.True(t, ok)
		assert.True(t, assignment.CategoryID().Equals(categoryID))
		assert.Equal(t, transaction.AssignedByUser, assignment.By())

		raised := manualTransaction.Events()
		require.Len(t, raised, 1)
		event, ok := raised[0].(transaction.Categorized)
		require.True(t, ok, "evento deve ser transaction.Categorized")
		assert.Equal(t, transaction.EventTypeCategorized, event.EventName())
	})

	t.Run("categoryID zero é recusado sem emitir evento", func(t *testing.T) {
		t.Parallel()

		manualTransaction := newValidManualTransaction(t)

		err := manualTransaction.Categorize(category.CategoryID{}, transaction.AssignedByUser)

		require.ErrorIs(t, err, transaction.ErrInvalidAssignment)
		_, ok := manualTransaction.Category()
		assert.False(t, ok, "falha não atribui categoria")
		assert.Empty(t, manualTransaction.Events(), "falha não gera fato")
	})

	t.Run("recategorizar substitui e re-emite o evento", func(t *testing.T) {
		t.Parallel()

		manualTransaction := newValidManualTransaction(t)
		firstCategory := category.NewCategoryID()
		secondCategory := category.NewCategoryID()

		require.NoError(t, manualTransaction.Categorize(firstCategory, transaction.AssignedByUser))
		require.NoError(t, manualTransaction.Categorize(secondCategory, transaction.AssignedByRule))

		assignment, ok := manualTransaction.Category()
		require.True(t, ok)
		assert.True(t, assignment.CategoryID().Equals(secondCategory), "última atribuição vence")
		assert.Equal(t, transaction.AssignedByRule, assignment.By())
		assert.Len(t, manualTransaction.Events(), 2, "cada categorização é um fato")
	})
}

// TestMarkReconciled — só transação de provider concilia; repetição é
// no-op sem evento.
func TestMarkReconciled(t *testing.T) {
	t.Parallel()

	newProviderTransaction := func(t *testing.T) *transaction.Transaction {
		t.Helper()
		providerTransaction, err := transaction.NewFromProvider(mustUserID(t, validUUID), account.NewAccountID(), mustMoney(t, -10_00, "BRL"), time.Now(), "Padaria", mustExternalRef(t))
		require.NoError(t, err)
		providerTransaction.ClearEvents() // isola o Imported da criação
		return providerTransaction
	}

	t.Run("concilia transação de provider e emite Reconciled", func(t *testing.T) {
		t.Parallel()

		providerTransaction := newProviderTransaction(t)

		require.NoError(t, providerTransaction.MarkReconciled())

		assert.True(t, providerTransaction.IsReconciled())
		raised := providerTransaction.Events()
		require.Len(t, raised, 1)
		assert.Equal(t, transaction.EventTypeReconciled, raised[0].EventName())
	})

	t.Run("transação manual não concilia", func(t *testing.T) {
		t.Parallel()

		manualTransaction := newValidManualTransaction(t)

		err := manualTransaction.MarkReconciled()

		require.ErrorIs(t, err, transaction.ErrNotReconcilable)
		assert.False(t, manualTransaction.IsReconciled())
		assert.Empty(t, manualTransaction.Events(), "falha não gera fato")
	})

	t.Run("conciliar de novo é idempotente sem evento novo", func(t *testing.T) {
		t.Parallel()

		providerTransaction := newProviderTransaction(t)
		require.NoError(t, providerTransaction.MarkReconciled())
		providerTransaction.ClearEvents()

		require.NoError(t, providerTransaction.MarkReconciled())

		assert.True(t, providerTransaction.IsReconciled())
		assert.Empty(t, providerTransaction.Events(), "repetição não é fato novo")
	})
}

// TestClearEvents — ciclo Save → Dispatch → ClearEvents + contrato de
// cópia do Events().
func TestClearEvents(t *testing.T) {
	t.Parallel()

	t.Run("limpa os eventos despachados", func(t *testing.T) {
		t.Parallel()

		manualTransaction := newValidManualTransaction(t)
		require.NoError(t, manualTransaction.Categorize(category.NewCategoryID(), transaction.AssignedByUser))
		require.Len(t, manualTransaction.Events(), 1)

		manualTransaction.ClearEvents()

		assert.Empty(t, manualTransaction.Events())
	})

	t.Run("mutar o slice retornado não afeta o aggregate", func(t *testing.T) {
		t.Parallel()

		manualTransaction := newValidManualTransaction(t)
		require.NoError(t, manualTransaction.Categorize(category.NewCategoryID(), transaction.AssignedByUser))

		leaked := manualTransaction.Events()
		leaked[0] = nil

		fresh := manualTransaction.Events()
		require.Len(t, fresh, 1)
		assert.NotNil(t, fresh[0], "Events() deve devolver cópia")
	})
}

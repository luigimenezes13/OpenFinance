package account_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helpers compartilhados por todos os _test.go deste package (arquivos do
// mesmo package de teste enxergam uns aos outros).

const validUUID = "550e8400-e29b-41d4-a716-446655440000"

func mustUserID(t *testing.T, raw string) shared.UserID {
	t.Helper()
	userID, err := shared.NewUserID(uuid.MustParse(raw))
	require.NoError(t, err)
	return userID
}

func mustCurrency(t *testing.T, code string) shared.Currency {
	t.Helper()
	currency, err := shared.NewCurrency(code)
	require.NoError(t, err)
	return currency
}

func mustMoney(t *testing.T, amount int64, code string) shared.Money {
	t.Helper()
	money, err := shared.NewMoney(amount, mustCurrency(t, code))
	require.NoError(t, err)
	return money
}

func mustBalance(t *testing.T, amount int64, code string, asOf time.Time) account.Balance {
	t.Helper()
	balance, err := account.NewBalance(mustMoney(t, amount, code), asOf)
	require.NoError(t, err)
	return balance
}

func newValidAccount(t *testing.T) *account.Account {
	t.Helper()
	validAccount, err := account.New(mustUserID(t, validUUID), "Conta Corrente", account.KindChecking, mustCurrency(t, "BRL"), account.NewManualSource())
	require.NoError(t, err)
	return validAccount
}

// TestNew cobre as invariantes do construtor.
func TestNew(t *testing.T) {
	t.Parallel()

	validUser := mustUserID(t, validUUID)
	brl := mustCurrency(t, "BRL")
	manual := account.NewManualSource()

	cases := []struct {
		name        string
		userID      shared.UserID
		accountName string
		kind        account.Kind
		currency    shared.Currency
		source      account.Source
		wantErr     error
	}{
		{"happy path", validUser, "Conta Corrente", account.KindChecking, brl, manual, nil},
		{"userID zero value", shared.UserID{}, "Conta", account.KindChecking, brl, manual, shared.ErrInvalidUserID},
		{"nome vazio", validUser, "", account.KindChecking, brl, manual, account.ErrInvalidName},
		{"nome só com espaços", validUser, "   ", account.KindChecking, brl, manual, account.ErrInvalidName},
		{"kind zero value", validUser, "Conta", account.Kind{}, brl, manual, account.ErrInvalidKind},
		{"currency zero value", validUser, "Conta", account.KindChecking, shared.Currency{}, manual, shared.ErrInvalidCurrency},
		{"source zero value", validUser, "Conta", account.KindChecking, brl, account.Source{}, account.ErrInvalidSource},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := account.New(tc.userID, tc.accountName, tc.kind, tc.currency, tc.source)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.False(t, got.ID().IsZero(), "New deve gerar identidade")
			assert.True(t, got.UserID().Equals(tc.userID))
			assert.Equal(t, tc.accountName, got.Name())

			initialBalance := got.Balance()
			assert.True(t, initialBalance.Money().IsZero(), "saldo inicial deve ser zero")
			assert.True(t, initialBalance.Currency().Equals(tc.currency))

			assert.Empty(t, got.Events(), "criar conta não emite evento")
		})
	}

	t.Run("nome é normalizado (trim)", func(t *testing.T) {
		t.Parallel()

		got, err := account.New(validUser, "  Conta Corrente  ", account.KindChecking, brl, manual)

		require.NoError(t, err)
		assert.Equal(t, "Conta Corrente", got.Name())
	})

	t.Run("cada conta nasce com identidade própria", func(t *testing.T) {
		t.Parallel()

		first := newValidAccount(t)
		second := newValidAccount(t)

		assert.False(t, first.ID().Equals(second.ID()))
	})
}

// TestUpdateBalance — mutação + emissão do evento, o coração do aggregate.
func TestUpdateBalance(t *testing.T) {
	t.Parallel()

	t.Run("troca o saldo e emite BalanceUpdated", func(t *testing.T) {
		t.Parallel()

		validAccount := newValidAccount(t)
		newBalance := mustBalance(t, 150_00, "BRL", time.Now())

		require.NoError(t, validAccount.UpdateBalance(newBalance))

		assert.Equal(t, int64(150_00), validAccount.Balance().Money().Amount())

		raised := validAccount.Events()
		require.Len(t, raised, 1)

		event, ok := raised[0].(account.BalanceUpdated)
		require.True(t, ok, "evento deve ser account.BalanceUpdated")
		assert.Equal(t, account.EventTypeBalanceUpdated, event.EventName())
		assert.True(t, event.AccountID().Equals(validAccount.ID()))
		assert.True(t, event.Previous().Money().IsZero(), "previous deve ser o saldo inicial zero")
		assert.Equal(t, int64(150_00), event.Current().Money().Amount())
		assert.False(t, event.OccurredAt().IsZero())
	})

	t.Run("saldo zero value é recusado sem emitir evento", func(t *testing.T) {
		t.Parallel()

		validAccount := newValidAccount(t)

		err := validAccount.UpdateBalance(account.Balance{})

		require.ErrorIs(t, err, account.ErrInvalidBalance)
		assert.Empty(t, validAccount.Events(), "falha não gera fato de domínio")
	})

	t.Run("moeda diferente é recusada", func(t *testing.T) {
		t.Parallel()

		validAccount := newValidAccount(t)
		usdBalance := mustBalance(t, 150_00, "USD", time.Now())

		err := validAccount.UpdateBalance(usdBalance)

		require.ErrorIs(t, err, account.ErrCurrencyMismatch)
		assert.Empty(t, validAccount.Events())
	})

	t.Run("saldo não volta no tempo", func(t *testing.T) {
		t.Parallel()

		validAccount := newValidAccount(t)
		staleBalance := mustBalance(t, 150_00, "BRL", time.Now().Add(-time.Hour))

		err := validAccount.UpdateBalance(staleBalance)

		require.ErrorIs(t, err, account.ErrStaleBalance)
		assert.Empty(t, validAccount.Events())
	})
}

// TestRename — validação reaproveitada do New.
func TestRename(t *testing.T) {
	t.Parallel()

	t.Run("nome válido troca e normaliza", func(t *testing.T) {
		t.Parallel()

		validAccount := newValidAccount(t)

		require.NoError(t, validAccount.Rename("  Conta Nova  "))

		assert.Equal(t, "Conta Nova", validAccount.Name())
		assert.Empty(t, validAccount.Events(), "renomear não emite evento no v1")
	})

	t.Run("nome vazio é recusado e mantém o atual", func(t *testing.T) {
		t.Parallel()

		validAccount := newValidAccount(t)

		err := validAccount.Rename("   ")

		require.ErrorIs(t, err, account.ErrInvalidName)
		assert.Equal(t, "Conta Corrente", validAccount.Name())
	})
}

// TestReconstitute — hidratação confia nos dados e não emite eventos.
func TestReconstitute(t *testing.T) {
	t.Parallel()

	id := account.NewAccountID()
	userID := mustUserID(t, validUUID)
	balance := mustBalance(t, 999_99, "BRL", time.Now())

	got := account.Reconstitute(id, userID, "Poupança", account.KindSavings, balance, account.NewManualSource())

	assert.True(t, got.ID().Equals(id))
	assert.Equal(t, "Poupança", got.Name())
	assert.Equal(t, int64(999_99), got.Balance().Money().Amount())
	assert.Empty(t, got.Events(), "rehidratar não é fato de domínio")
}

// TestClearEvents — o ciclo Save → Dispatch → ClearEvents do use case,
// mais o contrato de que Events() devolve cópia.
func TestClearEvents(t *testing.T) {
	t.Parallel()

	t.Run("limpa os eventos despachados", func(t *testing.T) {
		t.Parallel()

		validAccount := newValidAccount(t)
		require.NoError(t, validAccount.UpdateBalance(mustBalance(t, 100_00, "BRL", time.Now())))
		require.Len(t, validAccount.Events(), 1)

		validAccount.ClearEvents()

		assert.Empty(t, validAccount.Events())
	})

	t.Run("mutar o slice retornado não afeta o aggregate", func(t *testing.T) {
		t.Parallel()

		validAccount := newValidAccount(t)
		require.NoError(t, validAccount.UpdateBalance(mustBalance(t, 100_00, "BRL", time.Now())))

		leaked := validAccount.Events()
		leaked[0] = nil

		fresh := validAccount.Events()
		require.Len(t, fresh, 1)
		assert.NotNil(t, fresh[0], "Events() deve devolver cópia, não o slice interno")
	})
}

// TestAccountIDFromUUID — o domínio recebe uuid.UUID já convertido pela
// borda; a invariante local é recusar o Nil com o sentinel do aggregate.
func TestAccountIDFromUUID(t *testing.T) {
	t.Parallel()

	t.Run("UUID válido", func(t *testing.T) {
		t.Parallel()

		got, err := account.AccountIDFromUUID(uuid.MustParse(validUUID))

		require.NoError(t, err)
		assert.Equal(t, validUUID, got.String())
	})

	t.Run("uuid.Nil é recusado", func(t *testing.T) {
		t.Parallel()

		_, err := account.AccountIDFromUUID(uuid.Nil)

		require.ErrorIs(t, err, account.ErrInvalidID)
	})
}

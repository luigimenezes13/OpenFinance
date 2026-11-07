package application_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

func TestCreateAccount(t *testing.T) {
	t.Parallel()

	t.Run("should save a zero balance account in the requested currency without any event, since being born at zero is not a balance change", func(t *testing.T) {
		t.Parallel()

		accounts := newFakeAccounts()
		outbox := &fakeOutbox{}
		useCase := application.NewCreateAccountUseCase(accounts, newTransactionRunner(&fakeUnitOfWork{}, outbox))
		ownerID := uuid.New()

		output, err := useCase.Execute(context.Background(), application.CreateAccountInput{
			UserID:   ownerID,
			Name:     "  Nubank  ",
			Kind:     "checking",
			Currency: "BRL",
		})

		require.NoError(t, err)
		assert.Equal(t, ownerID.String(), output.UserID)
		assert.Equal(t, "Nubank", output.Name, "the aggregate trims the name")
		assert.Equal(t, "checking", output.Kind)
		assert.Zero(t, output.Balance, "a new account is born at zero")
		assert.Equal(t, "BRL", output.Currency)
		assert.NotEmpty(t, output.ID)
		require.Len(t, accounts.saved, 1, "the use case must persist the account")
		saved := accounts.saved[0]
		assert.Equal(t, output.ID, saved.ID().String())
		assert.Empty(t, outbox.recorded, "creating an account records no event")
	})

	t.Run("should return the repository error as is, since translating it is the HTTP edge's job", func(t *testing.T) {
		t.Parallel()

		accounts := newFakeAccounts()
		accounts.saveErr = errInfra
		useCase := application.NewCreateAccountUseCase(accounts, newPassThroughRunner())

		_, err := useCase.Execute(context.Background(), application.CreateAccountInput{
			UserID:   uuid.New(),
			Name:     "Account",
			Kind:     "savings",
			Currency: "BRL",
		})

		require.ErrorIs(t, err, errInfra)
	})
}

func TestCreateAccountRefusal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   application.CreateAccountInput
		wantErr error
	}{
		{
			name:    "should refuse a nil user",
			input:   application.CreateAccountInput{UserID: uuid.Nil, Name: "Account", Kind: "checking", Currency: "BRL"},
			wantErr: shared.ErrInvalidUserID,
		},
		{
			name:    "should refuse a kind outside the vocabulary",
			input:   application.CreateAccountInput{UserID: uuid.New(), Name: "Account", Kind: "crypto", Currency: "BRL"},
			wantErr: account.ErrInvalidKind,
		},
		{
			name:    "should refuse an unsupported currency",
			input:   application.CreateAccountInput{UserID: uuid.New(), Name: "Account", Kind: "checking", Currency: "JPY"},
			wantErr: shared.ErrInvalidCurrency,
		},
		{
			name:    "should refuse a lowercase currency",
			input:   application.CreateAccountInput{UserID: uuid.New(), Name: "Account", Kind: "checking", Currency: "brl"},
			wantErr: shared.ErrInvalidCurrency,
		},
		{
			name:    "should refuse a blank name",
			input:   application.CreateAccountInput{UserID: uuid.New(), Name: "   ", Kind: "checking", Currency: "BRL"},
			wantErr: account.ErrInvalidName,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			accounts := newFakeAccounts()
			useCase := application.NewCreateAccountUseCase(accounts, newPassThroughRunner())

			_, err := useCase.Execute(context.Background(), testCase.input)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, accounts.saved, "an invalid input never reaches the repository")
		})
	}
}

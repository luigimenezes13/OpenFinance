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

// TestCreateAccountUseCase_Execute cobre o caminho feliz: conta nasce
// zerada, na moeda pedida, e chega ao repositório.
func TestCreateAccountUseCase_Execute(t *testing.T) {
	t.Parallel()

	accounts := newFakeAccounts()
	useCase := application.NewCreateAccountUseCase(accounts)
	ownerID := uuid.New()

	output, err := useCase.Execute(context.Background(), application.CreateAccountInput{
		UserID:   ownerID,
		Name:     "  Nubank  ",
		Kind:     "checking",
		Currency: "BRL",
	})

	require.NoError(t, err)
	assert.Equal(t, ownerID.String(), output.UserID)
	assert.Equal(t, "Nubank", output.Name, "o aggregate normaliza o nome (trim)")
	assert.Equal(t, "checking", output.Kind)
	assert.Zero(t, output.Balance, "conta nova nasce zerada")
	assert.Equal(t, "BRL", output.Currency)
	assert.NotEmpty(t, output.ID)

	require.Len(t, accounts.saved, 1, "o use case tem que persistir a conta")
	saved := accounts.saved[0]
	assert.Equal(t, output.ID, saved.ID().String())
	assert.Empty(t, saved.Events(), "criar conta não emite evento: nascer zerada não é mudança de saldo")
}

// TestCreateAccountUseCase_Execute_Recusa verifica que cada input inválido
// morre no VO/aggregate certo — o use case só repassa o sentinel, e é isso
// que a borda HTTP usa pra escolher o status.
func TestCreateAccountUseCase_Execute_Recusa(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   application.CreateAccountInput
		wantErr error
	}{
		{
			name:    "usuário nil",
			input:   application.CreateAccountInput{UserID: uuid.Nil, Name: "Conta", Kind: "checking", Currency: "BRL"},
			wantErr: shared.ErrInvalidUserID,
		},
		{
			name:    "kind fora do vocabulário",
			input:   application.CreateAccountInput{UserID: uuid.New(), Name: "Conta", Kind: "cripto", Currency: "BRL"},
			wantErr: account.ErrInvalidKind,
		},
		{
			name:    "moeda não suportada",
			input:   application.CreateAccountInput{UserID: uuid.New(), Name: "Conta", Kind: "checking", Currency: "JPY"},
			wantErr: shared.ErrInvalidCurrency,
		},
		{
			name:    "moeda em lowercase",
			input:   application.CreateAccountInput{UserID: uuid.New(), Name: "Conta", Kind: "checking", Currency: "brl"},
			wantErr: shared.ErrInvalidCurrency,
		},
		{
			name:    "nome só com espaços",
			input:   application.CreateAccountInput{UserID: uuid.New(), Name: "   ", Kind: "checking", Currency: "BRL"},
			wantErr: account.ErrInvalidName,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			accounts := newFakeAccounts()
			useCase := application.NewCreateAccountUseCase(accounts)

			_, err := useCase.Execute(context.Background(), testCase.input)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, accounts.saved, "input inválido não pode chegar ao repositório")
		})
	}
}

// TestCreateAccountUseCase_Execute_PropagaErroDoRepositorio garante que erro
// de infraestrutura sobe cru: traduzir é papel da borda HTTP, não do use case.
func TestCreateAccountUseCase_Execute_PropagaErroDoRepositorio(t *testing.T) {
	t.Parallel()

	accounts := newFakeAccounts()
	accounts.saveErr = errInfra
	useCase := application.NewCreateAccountUseCase(accounts)

	_, err := useCase.Execute(context.Background(), application.CreateAccountInput{
		UserID:   uuid.New(),
		Name:     "Conta",
		Kind:     "savings",
		Currency: "BRL",
	})

	require.ErrorIs(t, err, errInfra)
}

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
)

// TestCreateCategoryUseCase_Execute cobre a categoria RAIZ (sem pai).
func TestCreateCategoryUseCase_Execute(t *testing.T) {
	t.Parallel()

	categories := newFakeCategories()
	useCase := application.NewCreateCategoryUseCase(categories)
	ownerID := uuid.New()

	output, err := useCase.Execute(context.Background(), application.CreateCategoryInput{
		UserID: ownerID,
		Name:   "  Alimentação  ",
	})

	require.NoError(t, err)
	assert.Equal(t, ownerID.String(), output.UserID)
	assert.Equal(t, "Alimentação", output.Name)
	assert.Nil(t, output.ParentID, "sem pai = categoria raiz")
	require.Len(t, categories.saved, 1)
}

// TestCreateCategoryUseCase_Execute_ComPai cobre a subcategoria: o ponteiro
// do input vira VO e volta na saída como string.
func TestCreateCategoryUseCase_Execute_ComPai(t *testing.T) {
	t.Parallel()

	categories := newFakeCategories()
	useCase := application.NewCreateCategoryUseCase(categories)
	parentID := uuid.New()

	output, err := useCase.Execute(context.Background(), application.CreateCategoryInput{
		UserID:   uuid.New(),
		Name:     "Restaurantes",
		ParentID: &parentID,
	})

	require.NoError(t, err)
	require.NotNil(t, output.ParentID)
	assert.Equal(t, parentID.String(), *output.ParentID)
}

// TestCreateCategoryUseCase_Execute_Recusa cobre os inputs inválidos.
func TestCreateCategoryUseCase_Execute_Recusa(t *testing.T) {
	t.Parallel()

	nilParent := uuid.Nil

	cases := []struct {
		name    string
		input   application.CreateCategoryInput
		wantErr error
	}{
		{
			name:    "usuário nil",
			input:   application.CreateCategoryInput{UserID: uuid.Nil, Name: "Alimentação"},
			wantErr: shared.ErrInvalidUserID,
		},
		{
			name:    "nome vazio",
			input:   application.CreateCategoryInput{UserID: uuid.New(), Name: ""},
			wantErr: category.ErrInvalidName,
		},
		{
			name:    "pai nil apontado",
			input:   application.CreateCategoryInput{UserID: uuid.New(), Name: "Restaurantes", ParentID: &nilParent},
			wantErr: category.ErrInvalidID,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			categories := newFakeCategories()
			useCase := application.NewCreateCategoryUseCase(categories)

			_, err := useCase.Execute(context.Background(), testCase.input)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, categories.saved)
		})
	}
}

// TestCreateCategoryUseCase_Execute_PropagaErroDoRepositorio: erro de infra
// sobe cru.
func TestCreateCategoryUseCase_Execute_PropagaErroDoRepositorio(t *testing.T) {
	t.Parallel()

	categories := newFakeCategories()
	categories.saveErr = errInfra
	useCase := application.NewCreateCategoryUseCase(categories)

	_, err := useCase.Execute(context.Background(), application.CreateCategoryInput{
		UserID: uuid.New(),
		Name:   "Transporte",
	})

	require.ErrorIs(t, err, errInfra)
}

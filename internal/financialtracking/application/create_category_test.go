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

func TestCreateCategory(t *testing.T) {
	t.Parallel()

	t.Run("should save a trimmed root category when no parent is given", func(t *testing.T) {
		t.Parallel()

		categories := newFakeCategories()
		useCase := application.NewCreateCategoryUseCase(categories, newPassThroughRunner())
		ownerID := uuid.New()

		output, err := useCase.Execute(context.Background(), application.CreateCategoryInput{
			UserID: ownerID,
			Name:   "  Food  ",
		})

		require.NoError(t, err)
		assert.Equal(t, ownerID.String(), output.UserID)
		assert.Equal(t, "Food", output.Name)
		assert.Nil(t, output.ParentID, "no parent means a root category")
		require.Len(t, categories.saved, 1)
	})

	t.Run("should save a subcategory and return the parent id as a string", func(t *testing.T) {
		t.Parallel()

		categories := newFakeCategories()
		useCase := application.NewCreateCategoryUseCase(categories, newPassThroughRunner())
		parentID := uuid.New()

		output, err := useCase.Execute(context.Background(), application.CreateCategoryInput{
			UserID:   uuid.New(),
			Name:     "Restaurants",
			ParentID: &parentID,
		})

		require.NoError(t, err)
		require.NotNil(t, output.ParentID)
		assert.Equal(t, parentID.String(), *output.ParentID)
	})

	t.Run("should return the repository error as is", func(t *testing.T) {
		t.Parallel()

		categories := newFakeCategories()
		categories.saveErr = errInfra
		useCase := application.NewCreateCategoryUseCase(categories, newPassThroughRunner())

		_, err := useCase.Execute(context.Background(), application.CreateCategoryInput{
			UserID: uuid.New(),
			Name:   "Transport",
		})

		require.ErrorIs(t, err, errInfra)
	})
}

func TestCreateCategoryRefusal(t *testing.T) {
	t.Parallel()

	nilParent := uuid.Nil

	cases := []struct {
		name    string
		input   application.CreateCategoryInput
		wantErr error
	}{
		{
			name:    "should refuse a nil user",
			input:   application.CreateCategoryInput{UserID: uuid.Nil, Name: "Food"},
			wantErr: shared.ErrInvalidUserID,
		},
		{
			name:    "should refuse a blank name",
			input:   application.CreateCategoryInput{UserID: uuid.New(), Name: ""},
			wantErr: category.ErrInvalidName,
		},
		{
			name:    "should refuse a nil parent id",
			input:   application.CreateCategoryInput{UserID: uuid.New(), Name: "Restaurants", ParentID: &nilParent},
			wantErr: category.ErrInvalidID,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			categories := newFakeCategories()
			useCase := application.NewCreateCategoryUseCase(categories, newPassThroughRunner())

			_, err := useCase.Execute(context.Background(), testCase.input)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, categories.saved)
		})
	}
}

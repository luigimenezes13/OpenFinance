package transaction_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

func mustPage(t *testing.T) kernel.Page {
	t.Helper()
	page, err := kernel.NewPage(kernel.DefaultLimit, 0)
	require.NoError(t, err)
	return page
}

// TestNewCriteriaSemFiltro: o mínimo é dono + janela.
func TestNewCriteriaSemFiltro(t *testing.T) {
	t.Parallel()

	userID := mustUserID(t, validUUID)

	criteria, err := transaction.NewCriteria(userID, mustPage(t))

	require.NoError(t, err)
	assert.True(t, criteria.UserID().Equals(userID))

	_, hasAccount := criteria.AccountID()
	assert.False(t, hasAccount)
	_, hasCategory := criteria.CategoryID()
	assert.False(t, hasCategory)
	_, hasFrom := criteria.From()
	assert.False(t, hasFrom)
}

// TestNewCriteriaComFiltros cobre as options funcionais.
func TestNewCriteriaComFiltros(t *testing.T) {
	t.Parallel()

	accountID := account.NewAccountID()
	categoryID := category.NewCategoryID()
	from := time.Now().Add(-30 * 24 * time.Hour)
	to := time.Now()

	criteria, err := transaction.NewCriteria(
		mustUserID(t, validUUID),
		mustPage(t),
		transaction.ForAccount(accountID),
		transaction.ForCategory(categoryID),
		transaction.InPeriod(from, to),
	)

	require.NoError(t, err)

	gotAccount, hasAccount := criteria.AccountID()
	require.True(t, hasAccount)
	assert.True(t, gotAccount.Equals(accountID))

	gotCategory, hasCategory := criteria.CategoryID()
	require.True(t, hasCategory)
	assert.True(t, gotCategory.Equals(categoryID))

	gotFrom, hasFrom := criteria.From()
	require.True(t, hasFrom)
	assert.Equal(t, from, gotFrom)
	gotTo, hasTo := criteria.To()
	require.True(t, hasTo)
	assert.Equal(t, to, gotTo)
}

// TestNewCriteriaPeriodoAberto: uma ponta só é filtro legítimo ("desde
// janeiro", "até hoje").
func TestNewCriteriaPeriodoAberto(t *testing.T) {
	t.Parallel()

	from := time.Now().Add(-24 * time.Hour)

	criteria, err := transaction.NewCriteria(
		mustUserID(t, validUUID), mustPage(t), transaction.InPeriod(from, time.Time{}))

	require.NoError(t, err)
	_, hasFrom := criteria.From()
	assert.True(t, hasFrom)
	_, hasTo := criteria.To()
	assert.False(t, hasTo)
}

// TestNewCriteriaRecusa cobre as invariantes.
func TestNewCriteriaRecusa(t *testing.T) {
	t.Parallel()

	validUser := mustUserID(t, validUUID)
	page := mustPage(t)

	cases := []struct {
		name    string
		userID  shared.UserID
		page    kernel.Page
		options []transaction.CriteriaOption
		wantErr error
	}{
		{
			name:    "usuário zerado",
			userID:  shared.UserID{},
			page:    page,
			wantErr: shared.ErrInvalidUserID,
		},
		{
			name:    "janela zerada",
			userID:  validUser,
			page:    kernel.Page{},
			wantErr: kernel.ErrInvalidPage,
		},
		{
			name:    "conta zerada no filtro",
			userID:  validUser,
			page:    page,
			options: []transaction.CriteriaOption{transaction.ForAccount(account.AccountID{})},
			wantErr: account.ErrInvalidID,
		},
		{
			name:    "categoria zerada no filtro",
			userID:  validUser,
			page:    page,
			options: []transaction.CriteriaOption{transaction.ForCategory(category.CategoryID{})},
			wantErr: category.ErrInvalidID,
		},
		{
			name:   "período invertido",
			userID: validUser,
			page:   page,
			options: []transaction.CriteriaOption{
				transaction.InPeriod(time.Now(), time.Now().Add(-24*time.Hour)),
			},
			// Devolver zero resultados para "de dezembro até janeiro" faria o
			// usuário concluir que não gastou nada no período.
			wantErr: transaction.ErrInvalidPeriod,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := transaction.NewCriteria(testCase.userID, testCase.page, testCase.options...)

			require.ErrorIs(t, err, testCase.wantErr)
		})
	}
}

package category_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// TestSnapshotRoundTrip cobre a categoria raiz COM regras: as regras estão
// dentro da fronteira do aggregate, então viajam no mesmo snapshot.
func TestSnapshotRoundTrip(t *testing.T) {
	t.Parallel()

	original := newValidCategory(t)
	require.NoError(t, original.AddRule(mustRule(t, "ifood")))
	require.NoError(t, original.AddRule(mustRule(t, "mercado")))

	rebuilt, err := category.FromSnapshot(original.Snapshot())

	require.NoError(t, err)
	assert.Equal(t, original.Snapshot(), rebuilt.Snapshot())
	assert.True(t, rebuilt.ID().Equals(original.ID()))

	originalRules := original.Rules()
	rebuiltRules := rebuilt.Rules()
	require.Len(t, rebuiltRules, 2)

	// A IDENTIDADE da regra tem que sobreviver: se o RuleID mudasse na
	// leitura, o RemoveRule do usuário miraria um id que não existe mais.
	firstOriginal := originalRules[0]
	firstRebuilt := rebuiltRules[0]
	originalRuleID := firstOriginal.ID()
	rebuiltRuleID := firstRebuilt.ID()
	assert.True(t, rebuiltRuleID.Equals(originalRuleID))
	assert.Equal(t, firstOriginal.Keyword(), firstRebuilt.Keyword())
}

// TestSnapshotRoundTripSubcategoria cobre o pai opcional.
func TestSnapshotRoundTripSubcategoria(t *testing.T) {
	t.Parallel()

	parentID := category.NewCategoryID()
	original, err := category.New(mustUserID(t, validUUID), "Restaurantes", &parentID)
	require.NoError(t, err)

	rebuilt, err := category.FromSnapshot(original.Snapshot())
	require.NoError(t, err)

	rebuiltParent := rebuilt.ParentID()
	require.NotNil(t, rebuiltParent)
	assert.True(t, rebuiltParent.Equals(parentID))
}

// TestFromSnapshotRecusaEstadoCorrompido cobre as linhas impossíveis,
// incluindo as duas que só existem por causa das regras.
func TestFromSnapshotRecusaEstadoCorrompido(t *testing.T) {
	t.Parallel()

	valid := newValidCategory(t).Snapshot()
	nilUUID := uuid.Nil

	cases := []struct {
		name    string
		corrupt func(snapshot *category.CategorySnapshot)
		wantErr error
	}{
		{
			name:    "id nil",
			corrupt: func(snapshot *category.CategorySnapshot) { snapshot.ID = uuid.Nil },
			wantErr: category.ErrInvalidID,
		},
		{
			name:    "usuário nil",
			corrupt: func(snapshot *category.CategorySnapshot) { snapshot.UserID = uuid.Nil },
			wantErr: shared.ErrInvalidUserID,
		},
		{
			name:    "nome vazio",
			corrupt: func(snapshot *category.CategorySnapshot) { snapshot.Name = "" },
			wantErr: category.ErrInvalidName,
		},
		{
			name:    "pai nil apontado",
			corrupt: func(snapshot *category.CategorySnapshot) { snapshot.ParentID = &nilUUID },
			wantErr: category.ErrInvalidID,
		},
		{
			name: "categoria como pai de si mesma",
			corrupt: func(snapshot *category.CategorySnapshot) {
				selfID := snapshot.ID
				snapshot.ParentID = &selfID
			},
			wantErr: category.ErrInvalidParent,
		},
		{
			name: "regra sem id",
			corrupt: func(snapshot *category.CategorySnapshot) {
				snapshot.Rules = []category.RuleSnapshot{{ID: uuid.Nil, Keyword: "ifood"}}
			},
			wantErr: category.ErrInvalidRuleID,
		},
		{
			name: "regra sem keyword",
			corrupt: func(snapshot *category.CategorySnapshot) {
				snapshot.Rules = []category.RuleSnapshot{{ID: uuid.New(), Keyword: "   "}}
			},
			wantErr: category.ErrInvalidKeyword,
		},
		{
			name: "keyword duplicado entre regras",
			corrupt: func(snapshot *category.CategorySnapshot) {
				snapshot.Rules = []category.RuleSnapshot{
					{ID: uuid.New(), Keyword: "ifood"},
					{ID: uuid.New(), Keyword: "ifood"},
				}
			},
			wantErr: category.ErrDuplicateRule,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			corrupted := valid
			testCase.corrupt(&corrupted)

			rebuilt, err := category.FromSnapshot(corrupted)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Nil(t, rebuilt)
		})
	}
}

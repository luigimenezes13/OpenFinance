//go:build integration

package entrepo_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
)

func mustRule(t *testing.T, keyword string) category.CategoryRule {
	t.Helper()
	rule, err := category.NewCategoryRule(keyword)
	require.NoError(t, err)
	return rule
}

// TestIntegrationCategoryRepositoryRoundTripComRegras é o teste central do
// único aggregate que ocupa duas tabelas: categoria e regras vão e voltam
// como UMA unidade (WithRules = o `include` do Prisma), com a identidade de
// cada regra preservada.
func TestIntegrationCategoryRepositoryRoundTripComRegras(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewCategoryRepository(testClient)
	original, err := category.New(mustUserID(t, uuid.New()), "Alimentação", nil)
	require.NoError(t, err)
	require.NoError(t, original.AddRule(mustRule(t, "ifood")))
	require.NoError(t, original.AddRule(mustRule(t, "mercado")))

	require.NoError(t, repository.Save(ctx, original))

	found, err := repository.FindByID(ctx, original.ID())
	require.NoError(t, err)

	assert.Equal(t, "Alimentação", found.Name())
	assert.Nil(t, found.ParentID(), "categoria raiz")

	// A comparação é por CONJUNTO (keyword -> id), não por posição: as
	// regras não têm ordem semântica, e comparar posicionalmente testaria
	// uma garantia que o domínio não dá — foi assim que este teste passou
	// por sorte até dois keywords empatarem no mesmo microssegundo.
	originalIDByKeyword := make(map[string]string, 2)
	for _, rule := range original.Rules() {
		ruleID := rule.ID()
		originalIDByKeyword[rule.Keyword()] = ruleID.String()
	}

	foundIDByKeyword := make(map[string]string, 2)
	for _, rule := range found.Rules() {
		ruleID := rule.ID()
		foundIDByKeyword[rule.Keyword()] = ruleID.String()
	}

	require.Len(t, foundIDByKeyword, 2)
	// A IDENTIDADE de cada regra sobrevive: senão o RemoveRule do usuário
	// miraria um id que não existe mais.
	assert.Equal(t, originalIDByKeyword, foundIDByKeyword)
}

// TestIntegrationCategoryRepositorySaveReplacesRules cobre a estratégia
// "apaga e regrava": regra removida no domínio desaparece do banco.
func TestIntegrationCategoryRepositorySaveReplacesRules(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewCategoryRepository(testClient)
	target, err := category.New(mustUserID(t, uuid.New()), "Alimentação", nil)
	require.NoError(t, err)
	require.NoError(t, target.AddRule(mustRule(t, "ifood")))
	require.NoError(t, target.AddRule(mustRule(t, "mercado")))
	require.NoError(t, repository.Save(ctx, target))

	rules := target.Rules()
	removed := rules[0]
	removedID := removed.ID()
	require.NoError(t, target.RemoveRule(removedID))
	require.NoError(t, target.AddRule(mustRule(t, "restaurante")))
	require.NoError(t, repository.Save(ctx, target))

	found, err := repository.FindByID(ctx, target.ID())
	require.NoError(t, err)

	foundKeywords := make([]string, 0, 2)
	for _, rule := range found.Rules() {
		foundKeywords = append(foundKeywords, rule.Keyword())
	}
	assert.ElementsMatch(t, []string{"mercado", "restaurante"}, foundKeywords)

	var ruleCount int
	require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM category_rules`).Scan(&ruleCount))
	assert.Equal(t, 2, ruleCount)
}

// TestIntegrationCategoryRepositorySubcategoria cobre o pai opcional (coluna
// anulável, sem edge self-referencial).
func TestIntegrationCategoryRepositorySubcategoria(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewCategoryRepository(testClient)
	ownerID := uuid.New()
	parent, err := category.New(mustUserID(t, ownerID), "Alimentação", nil)
	require.NoError(t, err)
	require.NoError(t, repository.Save(ctx, parent))

	parentID := parent.ID()
	child, err := category.New(mustUserID(t, ownerID), "Restaurantes", &parentID)
	require.NoError(t, err)
	require.NoError(t, repository.Save(ctx, child))

	found, err := repository.FindByID(ctx, child.ID())
	require.NoError(t, err)

	foundParent := found.ParentID()
	require.NotNil(t, foundParent)
	assert.True(t, foundParent.Equals(parentID))
}

// TestIntegrationCategoryRepositoryRulesCascade prova que o único edge do
// schema virou FK com CASCADE de verdade: apagou a categoria, as regras vão
// com ela. É o contraste com as referências entre aggregates, que não têm FK.
func TestIntegrationCategoryRepositoryRulesCascade(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewCategoryRepository(testClient)
	target, err := category.New(mustUserID(t, uuid.New()), "Transporte", nil)
	require.NoError(t, err)
	require.NoError(t, target.AddRule(mustRule(t, "uber")))
	require.NoError(t, repository.Save(ctx, target))

	categoryID := target.ID()
	_, err = testDB.ExecContext(ctx, `DELETE FROM categories WHERE id = $1`, categoryID.UUID())
	require.NoError(t, err)

	var ruleCount int
	require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM category_rules`).Scan(&ruleCount))
	assert.Zero(t, ruleCount)
}

// TestIntegrationCategoryRepositoryNotFound: ausência → sentinel do
// aggregate.
func TestIntegrationCategoryRepositoryNotFound(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewCategoryRepository(testClient)
	missingID, err := category.CategoryIDFromUUID(uuid.New())
	require.NoError(t, err)

	found, err := repository.FindByID(ctx, missingID)

	require.ErrorIs(t, err, category.ErrNotFound)
	assert.Nil(t, found)
}

// TestIntegrationCategoryRepositorySaveRollback prova a transação do Ent: se
// a escrita das regras falhar, a categoria E as regras da mesma operação NÃO
// podem ficar no banco.
//
// Como forçar a falha sem inventar aggregate inválido: gravamos uma
// categoria vizinha e plantamos nela uma linha de regra com o MESMO RuleID
// da segunda regra da categoria alvo. O PRIMARY KEY recusa, e o rollback tem
// que levar tudo.
func TestIntegrationCategoryRepositorySaveRollback(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewCategoryRepository(testClient)
	ownerID := uuid.New()

	neighbour, err := category.New(mustUserID(t, ownerID), "Vizinha", nil)
	require.NoError(t, err)
	require.NoError(t, repository.Save(ctx, neighbour))

	target, err := category.New(mustUserID(t, ownerID), "Lazer", nil)
	require.NoError(t, err)
	require.NoError(t, target.AddRule(mustRule(t, "cinema")))
	require.NoError(t, target.AddRule(mustRule(t, "netflix")))

	targetRules := target.Rules()
	secondRule := targetRules[1]
	collidingID := secondRule.ID()
	neighbourID := neighbour.ID()
	_, err = testDB.ExecContext(ctx,
		`INSERT INTO category_rules (id, category_id, keyword, created_at) VALUES ($1, $2, $3, now())`,
		collidingID.UUID(), neighbourID.UUID(), "colidente")
	require.NoError(t, err)

	require.Error(t, repository.Save(ctx, target), "PK duplicada nas regras tem que falhar")

	found, err := repository.FindByID(ctx, target.ID())
	require.ErrorIs(t, err, category.ErrNotFound, "o upsert da categoria foi desfeito junto")
	assert.Nil(t, found)

	var cinemaCount int
	require.NoError(t, testDB.QueryRowContext(ctx,
		`SELECT count(*) FROM category_rules WHERE keyword = $1`, "cinema").Scan(&cinemaCount))
	assert.Zero(t, cinemaCount, "rollback desfaz a regra que já tinha entrado")

	var categoryCount int
	require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM categories`).Scan(&categoryCount))
	assert.Equal(t, 1, categoryCount, "a vizinha, de uma transação anterior, segue lá")
}

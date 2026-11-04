//go:build integration

package entrepo_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
)

func mustRule(t *testing.T, keyword string) category.CategoryRule {
	t.Helper()
	rule, err := category.NewCategoryRule(keyword)
	require.NoError(t, err)
	return rule
}

// ruleIDsByKeyword indexes the rules by keyword so they are compared as a set:
// rules have no semantic order, and a positional comparison would only pass
// by luck until two keywords tie on the same microsecond.
func ruleIDsByKeyword(rules []category.CategoryRule) map[string]string {
	idsByKeyword := make(map[string]string, len(rules))
	for _, rule := range rules {
		ruleID := rule.ID()
		idsByKeyword[rule.Keyword()] = ruleID.String()
	}
	return idsByKeyword
}

func TestCategoryRepositoryIntegration(t *testing.T) {
	ctx := context.Background()

	t.Run("should round-trip the category and its rules as one unit, preserving each rule identity", func(t *testing.T) {
		truncateAll(t)
		repository := newCategoryRepository()
		original, err := category.New(mustUserID(t, uuid.New()), "Food", nil)
		require.NoError(t, err)
		require.NoError(t, original.AddRule(mustRule(t, "ifood")))
		require.NoError(t, original.AddRule(mustRule(t, "grocery")))

		require.NoError(t, repository.Save(ctx, original))

		found, err := repository.FindByID(ctx, original.ID())
		require.NoError(t, err)

		assert.Equal(t, "Food", found.Name())
		assert.Nil(t, found.ParentID(), "root category")

		foundIDByKeyword := ruleIDsByKeyword(found.Rules())
		require.Len(t, foundIDByKeyword, 2)
		assert.Equal(t, ruleIDsByKeyword(original.Rules()), foundIDByKeyword,
			"rule ids survive, otherwise RemoveRule would target an id that no longer exists")
	})

	t.Run("should replace the stored rules so a rule removed in the domain disappears from the database", func(t *testing.T) {
		truncateAll(t)
		repository := newCategoryRepository()
		target, err := category.New(mustUserID(t, uuid.New()), "Food", nil)
		require.NoError(t, err)
		require.NoError(t, target.AddRule(mustRule(t, "ifood")))
		require.NoError(t, target.AddRule(mustRule(t, "grocery")))
		require.NoError(t, repository.Save(ctx, target))

		rules := target.Rules()
		removed := rules[0]
		removedID := removed.ID()
		require.NoError(t, target.RemoveRule(removedID))
		require.NoError(t, target.AddRule(mustRule(t, "restaurant")))
		require.NoError(t, repository.Save(ctx, target))

		found, err := repository.FindByID(ctx, target.ID())
		require.NoError(t, err)

		foundKeywords := make([]string, 0, 2)
		for _, rule := range found.Rules() {
			foundKeywords = append(foundKeywords, rule.Keyword())
		}
		assert.ElementsMatch(t, []string{"grocery", "restaurant"}, foundKeywords)

		var ruleCount int
		require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM category_rules`).Scan(&ruleCount))
		assert.Equal(t, 2, ruleCount)
	})

	t.Run("should round-trip the optional parent of a subcategory", func(t *testing.T) {
		truncateAll(t)
		repository := newCategoryRepository()
		ownerID := uuid.New()
		parent, err := category.New(mustUserID(t, ownerID), "Food", nil)
		require.NoError(t, err)
		require.NoError(t, repository.Save(ctx, parent))

		parentID := parent.ID()
		child, err := category.New(mustUserID(t, ownerID), "Restaurants", &parentID)
		require.NoError(t, err)
		require.NoError(t, repository.Save(ctx, child))

		found, err := repository.FindByID(ctx, child.ID())
		require.NoError(t, err)

		foundParent := found.ParentID()
		require.NotNil(t, foundParent)
		assert.True(t, foundParent.Equals(parentID))
	})

	t.Run("should cascade the rules when the category row is deleted, unlike references between aggregates", func(t *testing.T) {
		truncateAll(t)
		repository := newCategoryRepository()
		target, err := category.New(mustUserID(t, uuid.New()), "Transport", nil)
		require.NoError(t, err)
		require.NoError(t, target.AddRule(mustRule(t, "uber")))
		require.NoError(t, repository.Save(ctx, target))

		categoryID := target.ID()
		_, err = testDB.ExecContext(ctx, `DELETE FROM categories WHERE id = $1`, categoryID.UUID())
		require.NoError(t, err)

		var ruleCount int
		require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM category_rules`).Scan(&ruleCount))
		assert.Zero(t, ruleCount)
	})

	t.Run("should translate a missing row into the aggregate not found sentinel", func(t *testing.T) {
		truncateAll(t)
		repository := newCategoryRepository()
		missingID, err := category.CategoryIDFromUUID(uuid.New())
		require.NoError(t, err)

		found, err := repository.FindByID(ctx, missingID)

		require.ErrorIs(t, err, category.ErrNotFound)
		assert.Nil(t, found)
	})

	t.Run("should roll back the category and its rules when a planted rule with a colliding primary key fails the save", func(t *testing.T) {
		truncateAll(t)
		repository := newCategoryRepository()
		ownerID := uuid.New()

		neighbour, err := category.New(mustUserID(t, ownerID), "Neighbour", nil)
		require.NoError(t, err)
		require.NoError(t, repository.Save(ctx, neighbour))

		target, err := category.New(mustUserID(t, ownerID), "Leisure", nil)
		require.NoError(t, err)
		require.NoError(t, target.AddRule(mustRule(t, "cinema")))
		require.NoError(t, target.AddRule(mustRule(t, "netflix")))

		targetRules := target.Rules()
		secondRule := targetRules[1]
		collidingID := secondRule.ID()
		neighbourID := neighbour.ID()
		_, err = testDB.ExecContext(ctx,
			`INSERT INTO category_rules (id, category_id, keyword, created_at) VALUES ($1, $2, $3, now())`,
			collidingID.UUID(), neighbourID.UUID(), "colliding")
		require.NoError(t, err)

		require.Error(t, repository.Save(ctx, target), "a duplicated primary key in the rules must fail")

		found, err := repository.FindByID(ctx, target.ID())
		require.ErrorIs(t, err, category.ErrNotFound, "the category upsert was rolled back too")
		assert.Nil(t, found)

		var cinemaCount int
		require.NoError(t, testDB.QueryRowContext(ctx,
			`SELECT count(*) FROM category_rules WHERE keyword = $1`, "cinema").Scan(&cinemaCount))
		assert.Zero(t, cinemaCount, "the rollback undoes the rule that had already been written")

		var categoryCount int
		require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM categories`).Scan(&categoryCount))
		assert.Equal(t, 1, categoryCount, "the neighbour, from an earlier transaction, is still there")
	})
}

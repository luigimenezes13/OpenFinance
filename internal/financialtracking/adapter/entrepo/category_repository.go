package entrepo

import (
	"context"
	"fmt"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo/ent"
	entcategory "github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo/ent/category"
	entcategoryrule "github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo/ent/categoryrule"
	domaincategory "github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
)

// CategoryRepository implementa category.Repository. É o único repositório
// que escreve em DUAS tabelas, porque a entity CategoryRule vive dentro da
// fronteira do aggregate Category — logo, dentro da MESMA transação.
type CategoryRepository struct {
	client *ent.Client
}

var _ domaincategory.Repository = (*CategoryRepository)(nil)

// NewCategoryRepository injeta o client por construtor.
func NewCategoryRepository(client *ent.Client) *CategoryRepository {
	return &CategoryRepository{client: client}
}

// Save persiste a categoria e suas regras como UMA unidade.
//
// A estratégia das regras é "apaga e regrava" dentro da transação, não diff
// campo a campo: o aggregate é a fonte da verdade do conjunto (o que ele tem
// É o correto), então reconciliar por diferença reimplementaria no adapter
// uma decisão que o root já tomou. Volume é de unidades por categoria; se um
// dia virar milhares, aí vale o diff.
func (r *CategoryRepository) Save(ctx context.Context, target *domaincategory.Category) error {
	snapshot := target.Snapshot()

	return withTx(ctx, r.client, func(tx *ent.Tx) error {
		builder := tx.Category.Create().
			SetID(snapshot.ID).
			SetUserID(snapshot.UserID).
			SetName(snapshot.Name)

		if snapshot.ParentID != nil {
			builder = builder.SetParentID(*snapshot.ParentID)
		}

		if err := builder.OnConflictColumns(entcategory.FieldID).UpdateNewValues().Exec(ctx); err != nil {
			return fmt.Errorf("entrepo: falha salvando categoria: %w", err)
		}

		if _, err := tx.CategoryRule.Delete().
			Where(entcategoryrule.CategoryIDEQ(snapshot.ID)).
			Exec(ctx); err != nil {
			return fmt.Errorf("entrepo: falha limpando regras da categoria: %w", err)
		}

		if len(snapshot.Rules) == 0 {
			return nil
		}

		// CreateBulk vira UM insert com várias linhas — uma ida ao banco em
		// vez de N.
		builders := make([]*ent.CategoryRuleCreate, 0, len(snapshot.Rules))
		for _, rule := range snapshot.Rules {
			builders = append(builders, tx.CategoryRule.Create().
				SetID(rule.ID).
				SetCategoryID(snapshot.ID).
				SetKeyword(rule.Keyword))
		}
		if _, err := tx.CategoryRule.CreateBulk(builders...).Save(ctx); err != nil {
			return fmt.Errorf("entrepo: falha salvando regras da categoria: %w", err)
		}

		return nil
	})
}

// FindByID hidrata a categoria e suas regras. Ausência → category.ErrNotFound.
//
// WithRules é o eager loading do Ent (o `include` do Prisma): é AQUI que o
// edge declarado no schema se paga — uma chamada, o Ent resolve as duas
// queries e devolve as regras em row.Edges.Rules, sem mapper de JOIN.
func (r *CategoryRepository) FindByID(ctx context.Context, id domaincategory.CategoryID) (*domaincategory.Category, error) {
	row, err := r.client.Category.Query().
		Where(entcategory.IDEQ(id.UUID())).
		WithRules(func(query *ent.CategoryRuleQuery) {
			// Ordenado por KEYWORD, não por created_at.
			//
			// As regras de uma categoria são um CONJUNTO, não uma lista: o
			// domínio deduplica por keyword e nenhuma regra tem prioridade
			// sobre outra. Ordenar por created_at prometeria preservar a
			// ordem de inserção — promessa que o banco não cumpre, porque
			// timestamptz trunca em microssegundo e duas regras adicionadas
			// no mesmo instante empatam, deixando a ordem final por conta
			// do id (uuid aleatório).
			//
			// keyword é único por categoria (o índice garante), então a
			// ordenação é determinística e ainda legível. Se a rules engine
			// (PR2/PR5) precisar de precedência, ela entra como campo
			// EXPLÍCITO no domínio, não como ordem implícita de inserção.
			query.Order(ent.Asc(entcategoryrule.FieldKeyword))
		}).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, domaincategory.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("entrepo: falha lendo categoria: %w", err)
	}

	return domaincategory.FromSnapshot(toCategorySnapshot(row))
}

// toCategorySnapshot traduz entity + edge carregado → Snapshot.
func toCategorySnapshot(row *ent.Category) domaincategory.CategorySnapshot {
	snapshot := domaincategory.CategorySnapshot{
		ID:       row.ID,
		UserID:   row.UserID,
		Name:     row.Name,
		ParentID: row.ParentID,
		Rules:    make([]domaincategory.RuleSnapshot, 0, len(row.Edges.Rules)),
	}

	for _, rule := range row.Edges.Rules {
		snapshot.Rules = append(snapshot.Rules, domaincategory.RuleSnapshot{
			ID:      rule.ID,
			Keyword: rule.Keyword,
		})
	}

	return snapshot
}

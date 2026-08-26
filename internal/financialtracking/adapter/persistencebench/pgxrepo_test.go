//go:build bench

// Implementações de referência em SQL cru sobre PGXPOOL — a API nativa do
// pgx, sem passar por database/sql.
//
// Existe pra separar dois custos que ficariam misturados numa comparação
// só: o overhead do ORM (Ent vs SQL cru) e o overhead da camada
// database/sql (que o Ent obrigatoriamente atravessa). Sem esta terceira
// variante, qualquer diferença medida seria atribuída ao "ORM" mesmo que
// parte dela venha do database/sql.
package persistencebench_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

type pgxAccountRepository struct{ pool *pgxpool.Pool }

var _ account.Repository = (*pgxAccountRepository)(nil)

func (r *pgxAccountRepository) Save(ctx context.Context, target *account.Account) error {
	snapshot := target.Snapshot()
	_, err := r.pool.Exec(ctx, accountUpsertSQL,
		snapshot.ID, snapshot.UserID, snapshot.Name, snapshot.Kind,
		snapshot.BalanceAmount, snapshot.BalanceCurrency, snapshot.BalanceAsOf,
		snapshot.SourceProvider, snapshot.SourceProviderAccountID)
	if err != nil {
		return fmt.Errorf("pgxrepo: falha salvando conta: %w", err)
	}
	return nil
}

func (r *pgxAccountRepository) FindByID(ctx context.Context, id account.AccountID) (*account.Account, error) {
	var snapshot account.AccountSnapshot
	err := r.pool.QueryRow(ctx, accountSelectSQL, id.UUID()).Scan(
		&snapshot.ID, &snapshot.UserID, &snapshot.Name, &snapshot.Kind,
		&snapshot.BalanceAmount, &snapshot.BalanceCurrency, &snapshot.BalanceAsOf,
		&snapshot.SourceProvider, &snapshot.SourceProviderAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, account.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("pgxrepo: falha lendo conta: %w", err)
	}
	return account.FromSnapshot(snapshot)
}

type pgxTransactionRepository struct{ pool *pgxpool.Pool }

var _ transaction.Repository = (*pgxTransactionRepository)(nil)

func (r *pgxTransactionRepository) Save(ctx context.Context, target *transaction.Transaction) error {
	snapshot := target.Snapshot()

	var assignedAt *time.Time
	if snapshot.CategoryID != nil {
		assignedAt = &snapshot.CategoryAssignedAt
	}

	_, err := r.pool.Exec(ctx, transactionUpsertSQL,
		snapshot.ID, snapshot.UserID, snapshot.AccountID, snapshot.Amount, snapshot.Currency,
		snapshot.OccurredAt, snapshot.Description, snapshot.Reconciled,
		snapshot.CategoryID, snapshot.CategoryAssignedBy, assignedAt,
		snapshot.ExternalRefProvider, snapshot.ExternalRefProviderTransactionID)
	if err != nil {
		return fmt.Errorf("pgxrepo: falha salvando transação: %w", err)
	}
	return nil
}

func (r *pgxTransactionRepository) FindByID(ctx context.Context, id transaction.TransactionID) (*transaction.Transaction, error) {
	var snapshot transaction.TransactionSnapshot
	var categoryID *uuid.UUID
	var assignedAt *time.Time

	err := r.pool.QueryRow(ctx, transactionSelectSQL, id.UUID()).Scan(
		&snapshot.ID, &snapshot.UserID, &snapshot.AccountID, &snapshot.Amount, &snapshot.Currency,
		&snapshot.OccurredAt, &snapshot.Description, &snapshot.Reconciled,
		&categoryID, &snapshot.CategoryAssignedBy, &assignedAt,
		&snapshot.ExternalRefProvider, &snapshot.ExternalRefProviderTransactionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, transaction.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("pgxrepo: falha lendo transação: %w", err)
	}

	snapshot.CategoryID = categoryID
	if assignedAt != nil {
		snapshot.CategoryAssignedAt = *assignedAt
	}
	return transaction.FromSnapshot(snapshot)
}

type pgxCategoryRepository struct{ pool *pgxpool.Pool }

var _ category.Repository = (*pgxCategoryRepository)(nil)

func (r *pgxCategoryRepository) Save(ctx context.Context, target *category.Category) error {
	snapshot := target.Snapshot()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("pgxrepo: falha abrindo transação: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, categoryUpsertSQL,
		snapshot.ID, snapshot.UserID, snapshot.Name, snapshot.ParentID); err != nil {
		return fmt.Errorf("pgxrepo: falha salvando categoria: %w", err)
	}

	if _, err := tx.Exec(ctx, categoryRulesDeleteSQL, snapshot.ID); err != nil {
		return fmt.Errorf("pgxrepo: falha limpando regras: %w", err)
	}

	if len(snapshot.Rules) > 0 {
		arguments := make([]any, 0, len(snapshot.Rules)*3)
		for _, rule := range snapshot.Rules {
			arguments = append(arguments, rule.ID, snapshot.ID, rule.Keyword)
		}
		if _, err := tx.Exec(ctx, categoryRulesInsertSQL(len(snapshot.Rules)), arguments...); err != nil {
			return fmt.Errorf("pgxrepo: falha salvando regras: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pgxrepo: falha no commit: %w", err)
	}
	return nil
}

func (r *pgxCategoryRepository) FindByID(ctx context.Context, id category.CategoryID) (*category.Category, error) {
	var snapshot category.CategorySnapshot
	err := r.pool.QueryRow(ctx, categorySelectSQL, id.UUID()).Scan(
		&snapshot.ID, &snapshot.UserID, &snapshot.Name, &snapshot.ParentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, category.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("pgxrepo: falha lendo categoria: %w", err)
	}

	rows, err := r.pool.Query(ctx, categoryRulesSelectSQL, id.UUID())
	if err != nil {
		return nil, fmt.Errorf("pgxrepo: falha lendo regras: %w", err)
	}
	defer rows.Close()

	snapshot.Rules = make([]category.RuleSnapshot, 0)
	for rows.Next() {
		var ruleID uuid.UUID
		var keyword string
		if err := rows.Scan(&ruleID, &keyword); err != nil {
			return nil, fmt.Errorf("pgxrepo: falha lendo regra: %w", err)
		}
		snapshot.Rules = append(snapshot.Rules, category.RuleSnapshot{ID: ruleID, Keyword: keyword})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgxrepo: falha iterando regras: %w", err)
	}

	return category.FromSnapshot(snapshot)
}

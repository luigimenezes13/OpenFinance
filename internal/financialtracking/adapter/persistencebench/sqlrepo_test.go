//go:build bench

// Implementações de referência em SQL ESCRITO À MÃO sobre *sql.DB — a
// mesma abordagem do antigo adapter/pgxrepo, reconstruída aqui só para o
// benchmark. Vive em arquivo _test.go de propósito: é código de medição,
// não de produção, e não deve virar um segundo adapter vivo pra manter.
//
// O SQL é equivalente ao que o Ent emite (upsert por id, multi-row insert
// nas regras), senão a comparação mediria SQL diferente em vez de mediria
// o custo da camada.
package persistencebench_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

// --- SQL compartilhado pelas duas implementações cruas -------------------

const accountUpsertSQL = `
INSERT INTO accounts (
    id, user_id, name, kind,
    balance_amount, balance_currency, balance_as_of,
    source_provider, source_provider_account_id, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), now())
ON CONFLICT (id) DO UPDATE SET
    name                       = EXCLUDED.name,
    kind                       = EXCLUDED.kind,
    balance_amount             = EXCLUDED.balance_amount,
    balance_currency           = EXCLUDED.balance_currency,
    balance_as_of              = EXCLUDED.balance_as_of,
    source_provider            = EXCLUDED.source_provider,
    source_provider_account_id = EXCLUDED.source_provider_account_id,
    updated_at                 = now()`

const accountSelectSQL = `
SELECT id, user_id, name, kind,
       balance_amount, balance_currency, balance_as_of,
       source_provider, source_provider_account_id
FROM accounts WHERE id = $1`

const transactionUpsertSQL = `
INSERT INTO transactions (
    id, user_id, account_id, amount, currency, occurred_at, description, reconciled,
    category_id, category_assigned_by, category_assigned_at,
    external_ref_provider, external_ref_provider_transaction_id, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, now(), now())
ON CONFLICT (id) DO UPDATE SET
    amount                               = EXCLUDED.amount,
    currency                             = EXCLUDED.currency,
    occurred_at                          = EXCLUDED.occurred_at,
    description                          = EXCLUDED.description,
    reconciled                           = EXCLUDED.reconciled,
    category_id                          = EXCLUDED.category_id,
    category_assigned_by                 = EXCLUDED.category_assigned_by,
    category_assigned_at                 = EXCLUDED.category_assigned_at,
    external_ref_provider                = EXCLUDED.external_ref_provider,
    external_ref_provider_transaction_id = EXCLUDED.external_ref_provider_transaction_id,
    updated_at                           = now()`

const transactionSelectSQL = `
SELECT id, user_id, account_id, amount, currency, occurred_at, description, reconciled,
       category_id, category_assigned_by, category_assigned_at,
       external_ref_provider, external_ref_provider_transaction_id
FROM transactions WHERE id = $1`

const categoryUpsertSQL = `
INSERT INTO categories (id, user_id, name, parent_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, now(), now())
ON CONFLICT (id) DO UPDATE SET
    name       = EXCLUDED.name,
    parent_id  = EXCLUDED.parent_id,
    updated_at = now()`

const categoryRulesDeleteSQL = `DELETE FROM category_rules WHERE category_id = $1`

const categorySelectSQL = `SELECT id, user_id, name, parent_id FROM categories WHERE id = $1`

const categoryRulesSelectSQL = `
SELECT id, keyword FROM category_rules WHERE category_id = $1 ORDER BY created_at, id`

// categoryRulesInsertSQL monta um insert multi-linha com N regras — o mesmo
// formato que o CreateBulk do Ent emite.
func categoryRulesInsertSQL(ruleCount int) string {
	values := make([]string, 0, ruleCount)
	for index := 0; index < ruleCount; index++ {
		base := index * 3
		values = append(values, fmt.Sprintf("($%d, $%d, $%d, now())", base+1, base+2, base+3))
	}
	return `INSERT INTO category_rules (id, category_id, keyword, created_at) VALUES ` +
		strings.Join(values, ", ")
}

// --- Implementações sobre *sql.DB ---------------------------------------

type sqlAccountRepository struct{ database *sql.DB }

var _ account.Repository = (*sqlAccountRepository)(nil)

func (r *sqlAccountRepository) Save(ctx context.Context, target *account.Account) error {
	snapshot := target.Snapshot()
	_, err := r.database.ExecContext(ctx, accountUpsertSQL,
		snapshot.ID, snapshot.UserID, snapshot.Name, snapshot.Kind,
		snapshot.BalanceAmount, snapshot.BalanceCurrency, snapshot.BalanceAsOf,
		snapshot.SourceProvider, snapshot.SourceProviderAccountID)
	if err != nil {
		return fmt.Errorf("sqlrepo: falha salvando conta: %w", err)
	}
	return nil
}

func (r *sqlAccountRepository) FindByID(ctx context.Context, id account.AccountID) (*account.Account, error) {
	var snapshot account.AccountSnapshot
	err := r.database.QueryRowContext(ctx, accountSelectSQL, id.UUID()).Scan(
		&snapshot.ID, &snapshot.UserID, &snapshot.Name, &snapshot.Kind,
		&snapshot.BalanceAmount, &snapshot.BalanceCurrency, &snapshot.BalanceAsOf,
		&snapshot.SourceProvider, &snapshot.SourceProviderAccountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, account.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: falha lendo conta: %w", err)
	}
	return account.FromSnapshot(snapshot)
}

type sqlTransactionRepository struct{ database *sql.DB }

var _ transaction.Repository = (*sqlTransactionRepository)(nil)

func (r *sqlTransactionRepository) Save(ctx context.Context, target *transaction.Transaction) error {
	snapshot := target.Snapshot()

	var assignedAt *time.Time
	if snapshot.CategoryID != nil {
		assignedAt = &snapshot.CategoryAssignedAt
	}

	_, err := r.database.ExecContext(ctx, transactionUpsertSQL,
		snapshot.ID, snapshot.UserID, snapshot.AccountID, snapshot.Amount, snapshot.Currency,
		snapshot.OccurredAt, snapshot.Description, snapshot.Reconciled,
		snapshot.CategoryID, snapshot.CategoryAssignedBy, assignedAt,
		snapshot.ExternalRefProvider, snapshot.ExternalRefProviderTransactionID)
	if err != nil {
		return fmt.Errorf("sqlrepo: falha salvando transação: %w", err)
	}
	return nil
}

func (r *sqlTransactionRepository) FindByID(ctx context.Context, id transaction.TransactionID) (*transaction.Transaction, error) {
	var snapshot transaction.TransactionSnapshot
	var categoryID *uuid.UUID
	var assignedAt *time.Time

	err := r.database.QueryRowContext(ctx, transactionSelectSQL, id.UUID()).Scan(
		&snapshot.ID, &snapshot.UserID, &snapshot.AccountID, &snapshot.Amount, &snapshot.Currency,
		&snapshot.OccurredAt, &snapshot.Description, &snapshot.Reconciled,
		&categoryID, &snapshot.CategoryAssignedBy, &assignedAt,
		&snapshot.ExternalRefProvider, &snapshot.ExternalRefProviderTransactionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, transaction.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: falha lendo transação: %w", err)
	}

	snapshot.CategoryID = categoryID
	if assignedAt != nil {
		snapshot.CategoryAssignedAt = *assignedAt
	}
	return transaction.FromSnapshot(snapshot)
}

type sqlCategoryRepository struct{ database *sql.DB }

var _ category.Repository = (*sqlCategoryRepository)(nil)

func (r *sqlCategoryRepository) Save(ctx context.Context, target *category.Category) error {
	snapshot := target.Snapshot()

	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlrepo: falha abrindo transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, categoryUpsertSQL,
		snapshot.ID, snapshot.UserID, snapshot.Name, snapshot.ParentID); err != nil {
		return fmt.Errorf("sqlrepo: falha salvando categoria: %w", err)
	}

	if _, err := tx.ExecContext(ctx, categoryRulesDeleteSQL, snapshot.ID); err != nil {
		return fmt.Errorf("sqlrepo: falha limpando regras: %w", err)
	}

	if len(snapshot.Rules) > 0 {
		arguments := make([]any, 0, len(snapshot.Rules)*3)
		for _, rule := range snapshot.Rules {
			arguments = append(arguments, rule.ID, snapshot.ID, rule.Keyword)
		}
		if _, err := tx.ExecContext(ctx, categoryRulesInsertSQL(len(snapshot.Rules)), arguments...); err != nil {
			return fmt.Errorf("sqlrepo: falha salvando regras: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlrepo: falha no commit: %w", err)
	}
	return nil
}

func (r *sqlCategoryRepository) FindByID(ctx context.Context, id category.CategoryID) (*category.Category, error) {
	var snapshot category.CategorySnapshot
	err := r.database.QueryRowContext(ctx, categorySelectSQL, id.UUID()).Scan(
		&snapshot.ID, &snapshot.UserID, &snapshot.Name, &snapshot.ParentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, category.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: falha lendo categoria: %w", err)
	}

	rows, err := r.database.QueryContext(ctx, categoryRulesSelectSQL, id.UUID())
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: falha lendo regras: %w", err)
	}
	defer rows.Close()

	snapshot.Rules = make([]category.RuleSnapshot, 0)
	for rows.Next() {
		var ruleID uuid.UUID
		var keyword string
		if err := rows.Scan(&ruleID, &keyword); err != nil {
			return nil, fmt.Errorf("sqlrepo: falha lendo regra: %w", err)
		}
		snapshot.Rules = append(snapshot.Rules, category.RuleSnapshot{ID: ruleID, Keyword: keyword})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlrepo: falha iterando regras: %w", err)
	}

	return category.FromSnapshot(snapshot)
}

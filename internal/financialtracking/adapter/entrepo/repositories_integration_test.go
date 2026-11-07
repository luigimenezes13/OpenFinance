//go:build integration

package entrepo_test

import (
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/platform/db"
)

func newAccountRepository() *entrepo.AccountRepository {
	return entrepo.NewAccountRepository(testClient)
}

func newTransactionRepository() *entrepo.TransactionRepository {
	return entrepo.NewTransactionRepository(testClient)
}

func newCategoryRepository() *entrepo.CategoryRepository {
	return entrepo.NewCategoryRepository(testClient, db.NewUnitOfWork(testDB))
}

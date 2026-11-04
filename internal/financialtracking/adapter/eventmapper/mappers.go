package eventmapper

import (
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

const firstVersion = 1

func All() []outbox.EventMapper {
	return []outbox.EventMapper{
		outbox.MapperFor(transaction.EventTypeImported, firstVersion, transactionImported),
		outbox.MapperFor(transaction.EventTypeCategorized, firstVersion, transactionCategorized),
		outbox.MapperFor(transaction.EventTypeReconciled, firstVersion, transactionReconciled),
		outbox.MapperFor(account.EventTypeBalanceUpdated, firstVersion, accountBalanceUpdated),
	}
}

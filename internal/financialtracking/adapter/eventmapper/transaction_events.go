package eventmapper

import (
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
)

type transactionImportedPayload struct {
	TransactionID         string `json:"transaction_id"`
	UserID                string `json:"user_id"`
	AccountID             string `json:"account_id"`
	Provider              string `json:"provider"`
	ProviderTransactionID string `json:"provider_transaction_id"`
}

func transactionImported(event transaction.Imported) transactionImportedPayload {
	transactionID := event.TransactionID()
	userID := event.UserID()
	accountID := event.AccountID()
	ref := event.Ref()

	return transactionImportedPayload{
		TransactionID:         transactionID.String(),
		UserID:                userID.String(),
		AccountID:             accountID.String(),
		Provider:              ref.Provider(),
		ProviderTransactionID: ref.ProviderTransactionID(),
	}
}

type transactionCategorizedPayload struct {
	TransactionID string `json:"transaction_id"`
	UserID        string `json:"user_id"`
	CategoryID    string `json:"category_id"`
	AssignedBy    string `json:"assigned_by"`
}

func transactionCategorized(event transaction.Categorized) transactionCategorizedPayload {
	transactionID := event.TransactionID()
	userID := event.UserID()
	categoryID := event.CategoryID()
	assignedBy := event.By()

	return transactionCategorizedPayload{
		TransactionID: transactionID.String(),
		UserID:        userID.String(),
		CategoryID:    categoryID.String(),
		AssignedBy:    assignedBy.String(),
	}
}

type transactionReconciledPayload struct {
	TransactionID string `json:"transaction_id"`
	UserID        string `json:"user_id"`
}

func transactionReconciled(event transaction.Reconciled) transactionReconciledPayload {
	transactionID := event.TransactionID()
	userID := event.UserID()

	return transactionReconciledPayload{
		TransactionID: transactionID.String(),
		UserID:        userID.String(),
	}
}

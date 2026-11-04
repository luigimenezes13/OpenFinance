package eventmapper

import (
	"time"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
)

type balancePayload struct {
	Amount   int64     `json:"amount"`
	Currency string    `json:"currency"`
	AsOf     time.Time `json:"as_of"`
}

type accountBalanceUpdatedPayload struct {
	AccountID string         `json:"account_id"`
	UserID    string         `json:"user_id"`
	Previous  balancePayload `json:"previous"`
	Current   balancePayload `json:"current"`
}

func accountBalanceUpdated(event account.BalanceUpdated) accountBalanceUpdatedPayload {
	accountID := event.AccountID()
	userID := event.UserID()

	return accountBalanceUpdatedPayload{
		AccountID: accountID.String(),
		UserID:    userID.String(),
		Previous:  toBalancePayload(event.Previous()),
		Current:   toBalancePayload(event.Current()),
	}
}

func toBalancePayload(balance account.Balance) balancePayload {
	money := balance.Money()
	currency := balance.Currency()

	return balancePayload{
		Amount:   money.Amount(),
		Currency: currency.Code(),
		AsOf:     balance.AsOf().UTC(),
	}
}

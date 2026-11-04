package eventmapper_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/eventmapper"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

const (
	transactionUUID = "0193a3b0-0000-7000-8000-000000000001"
	userUUID        = "0193a3b0-0000-7000-8000-000000000002"
	accountUUID     = "0193a3b0-0000-7000-8000-000000000003"
	categoryUUID    = "0193a3b0-0000-7000-8000-000000000004"
)

type fixtures struct {
	t *testing.T
}

func (f fixtures) transactionID() transaction.TransactionID {
	id, err := transaction.TransactionIDFromUUID(uuid.MustParse(transactionUUID))
	f.must(err)
	return id
}

func (f fixtures) userID() shared.UserID {
	id, err := shared.NewUserID(uuid.MustParse(userUUID))
	f.must(err)
	return id
}

func (f fixtures) accountID() account.AccountID {
	id, err := account.AccountIDFromUUID(uuid.MustParse(accountUUID))
	f.must(err)
	return id
}

func (f fixtures) categoryID() category.CategoryID {
	id, err := category.CategoryIDFromUUID(uuid.MustParse(categoryUUID))
	f.must(err)
	return id
}

func (f fixtures) balance(amount int64, asOf time.Time) account.Balance {
	currency, err := shared.NewCurrency("BRL")
	f.must(err)
	money, err := shared.NewMoney(amount, currency)
	f.must(err)
	balance, err := account.NewBalance(money, asOf)
	f.must(err)
	return balance
}

func (f fixtures) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("fixture: %v", err)
	}
}

func seal(t *testing.T, event events.Event) events.Envelope {
	t.Helper()

	mappers, err := outbox.NewMappers(eventmapper.All()...)
	if err != nil {
		t.Fatalf("new mappers: %v", err)
	}
	envelope, err := mappers.Seal(event)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	return envelope
}

func assertEnvelope(t *testing.T, envelope events.Envelope, wantName string, wantPayload string) {
	t.Helper()

	if envelope.Name != wantName {
		t.Errorf("name = %q, want %q", envelope.Name, wantName)
	}
	if envelope.Version != 1 {
		t.Errorf("version = %d, want 1", envelope.Version)
	}
	if string(envelope.Payload) != wantPayload {
		t.Errorf("payload\n got: %s\nwant: %s", envelope.Payload, wantPayload)
	}
}

func TestFinancialTrackingEventMappers(t *testing.T) {
	t.Run("should map a transaction imported from a provider", func(t *testing.T) {
		f := fixtures{t: t}
		ref, err := transaction.NewExternalRef("pluggy", "provider-tx-9")
		f.must(err)

		envelope := seal(t, transaction.NewImported(f.transactionID(), f.userID(), f.accountID(), ref))

		assertEnvelope(t, envelope, "financialtracking.transaction.imported",
			`{"transaction_id":"`+transactionUUID+`","user_id":"`+userUUID+`","account_id":"`+accountUUID+
				`","provider":"pluggy","provider_transaction_id":"provider-tx-9"}`)
	})

	t.Run("should map a categorized transaction", func(t *testing.T) {
		f := fixtures{t: t}
		assignedBy, err := transaction.NewAssignedBy("rule")
		f.must(err)

		envelope := seal(t, transaction.NewCategorized(f.transactionID(), f.userID(), f.categoryID(), assignedBy))

		assertEnvelope(t, envelope, "financialtracking.transaction.categorized",
			`{"transaction_id":"`+transactionUUID+`","user_id":"`+userUUID+`","category_id":"`+categoryUUID+
				`","assigned_by":"rule"}`)
	})

	t.Run("should map a reconciled transaction", func(t *testing.T) {
		f := fixtures{t: t}

		envelope := seal(t, transaction.NewReconciled(f.transactionID(), f.userID()))

		assertEnvelope(t, envelope, "financialtracking.transaction.reconciled",
			`{"transaction_id":"`+transactionUUID+`","user_id":"`+userUUID+`"}`)
	})

	t.Run("should map a balance update with both balances in UTC cents", func(t *testing.T) {
		f := fixtures{t: t}
		saoPaulo := time.FixedZone("BRT", -3*60*60)
		previous := f.balance(-1500, time.Date(2026, 11, 1, 9, 0, 0, 0, saoPaulo))
		current := f.balance(327360, time.Date(2026, 11, 4, 9, 30, 0, 0, saoPaulo))

		envelope := seal(t, account.NewBalanceUpdated(f.accountID(), f.userID(), previous, current))

		assertEnvelope(t, envelope, "financialtracking.account.balance_updated",
			`{"account_id":"`+accountUUID+`","user_id":"`+userUUID+`",`+
				`"previous":{"amount":-1500,"currency":"BRL","as_of":"2026-11-01T12:00:00Z"},`+
				`"current":{"amount":327360,"currency":"BRL","as_of":"2026-11-04T12:30:00Z"}}`)
	})

	t.Run("should cover every financial tracking event", func(t *testing.T) {
		mapped := map[string]bool{}
		for _, mapper := range eventmapper.All() {
			mapped[mapper.EventName()] = true
		}

		for _, eventName := range []string{
			transaction.EventTypeImported,
			transaction.EventTypeCategorized,
			transaction.EventTypeReconciled,
			account.EventTypeBalanceUpdated,
		} {
			if !mapped[eventName] {
				t.Errorf("%s has no mapper", eventName)
			}
		}
	})
}

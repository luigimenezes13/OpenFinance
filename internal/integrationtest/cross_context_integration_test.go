//go:build integration

package integrationtest

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/eventmapper"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/openfinance/mockprovider"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	identityentrepo "github.com/luigimenezes13/financial-manager/internal/identity/adapter/entrepo"
	identityeventmapper "github.com/luigimenezes13/financial-manager/internal/identity/adapter/eventmapper"
	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
	"github.com/luigimenezes13/financial-manager/internal/platform/db"
	"github.com/luigimenezes13/financial-manager/internal/platform/outbox"
)

const (
	isolatedSchema = "cross_context_integration"
	migrationsRoot = "../../migrations"
)

var errSaveFailed = errors.New("save failed")

type stack struct {
	database          *sql.DB
	transactionRunner *kernel.TransactionRunner
	accounts          *entrepo.AccountRepository
	transactions      *entrepo.TransactionRepository
	users             *identityentrepo.UserRepository
}

type silentSubscriber struct{}

func (silentSubscriber) SubscriberName() string { return "test.silent" }

func (silentSubscriber) Handle(context.Context, events.Envelope) error { return nil }

func newStack(t *testing.T) stack {
	t.Helper()

	database := connectToIsolatedSchema(t)
	applyMigrations(t, database, "financialtracking", "identity", "river")

	mappers, err := outbox.NewMappers(append(eventmapper.All(), identityeventmapper.All()...)...)
	if err != nil {
		t.Fatalf("new mappers: %v", err)
	}
	subscriptions := outbox.NewSubscriptions()
	if err := subscriptions.Subscribe(silentSubscriber{}, mappers.EventNames()...); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	queue, err := river.NewClient(riverdatabasesql.New(database), &river.Config{})
	if err != nil {
		t.Fatalf("new river client: %v", err)
	}

	transactionRunner := kernel.NewTransactionRunner(db.NewUnitOfWork(database), outbox.NewRecorder(mappers, subscriptions, queue))
	financialTrackingClient := entrepo.NewClient(database)
	identityClient := identityentrepo.NewClient(database)

	return stack{
		database:          database,
		transactionRunner: transactionRunner,
		accounts:          entrepo.NewAccountRepository(financialTrackingClient),
		transactions:      entrepo.NewTransactionRepository(financialTrackingClient),
		users:             identityentrepo.NewUserRepository(identityClient),
	}
}

func connectToIsolatedSchema(t *testing.T) *sql.DB {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("TEST_DATABASE_URL is empty; run make test-integration")
	}
	ctx := context.Background()

	admin, err := db.NewSQLDB(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+isolatedSchema+" CASCADE; CREATE SCHEMA "+isolatedSchema); err != nil {
		t.Fatalf("recreate schema: %v", err)
	}

	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse database url: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", isolatedSchema)
	parsed.RawQuery = query.Encode()

	database, err := db.NewSQLDB(ctx, parsed.String())
	if err != nil {
		t.Fatalf("connect to schema: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func applyMigrations(t *testing.T, database *sql.DB, directories ...string) {
	t.Helper()

	for _, directory := range directories {
		files, err := filepath.Glob(filepath.Join(migrationsRoot, directory, "*.sql"))
		if err != nil {
			t.Fatalf("list %s migrations: %v", directory, err)
		}
		sort.Strings(files)
		for _, file := range files {
			statements, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			if _, err := database.ExecContext(context.Background(), string(statements)); err != nil {
				t.Fatalf("apply %s: %v", file, err)
			}
		}
	}
}

func (s stack) count(t *testing.T, query string, arguments ...any) int {
	t.Helper()

	var total int
	if err := s.database.QueryRowContext(context.Background(), query, arguments...).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	return total
}

func (s stack) connectedAccount(t *testing.T) *account.Account {
	t.Helper()

	userID, err := shared.NewUserID(uuid.New())
	if err != nil {
		t.Fatalf("user id: %v", err)
	}
	currency, err := shared.NewCurrency("BRL")
	if err != nil {
		t.Fatalf("currency: %v", err)
	}
	source, err := account.NewOpenFinanceSource("mock", "provider-account-"+uuid.NewString())
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	connected, err := account.New(userID, "Checking Account", account.KindChecking, currency, source)
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	err = s.transactionRunner.Begin(context.Background(), func(ctx context.Context, transactionContext kernel.TransactionContext) error {
		transactionContext.RecordEventsOf(connected)
		return s.accounts.Save(ctx, connected)
	})
	if err != nil {
		t.Fatalf("save account: %v", err)
	}
	return connected
}

func registeredUser(t *testing.T) *identity.User {
	t.Helper()

	email, err := identity.NewEmail("ana-" + uuid.NewString() + "@example.com")
	if err != nil {
		t.Fatalf("email: %v", err)
	}
	external, err := identity.NewExternalIdentity("google", uuid.NewString())
	if err != nil {
		t.Fatalf("external identity: %v", err)
	}
	avatar, err := identity.NewAvatarURL("https://example.com/ana.png")
	if err != nil {
		t.Fatalf("avatar: %v", err)
	}
	user, err := identity.Register(email, "Ana", avatar, external)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return user
}

type failingOnSave struct {
	transaction.Repository
	failOn int
	saves  int
}

func (f *failingOnSave) Save(ctx context.Context, target *transaction.Transaction) error {
	f.saves++
	if f.saves == f.failOn {
		return errSaveFailed
	}
	return f.Repository.Save(ctx, target)
}

func importInput(connected *account.Account) application.ImportFromProviderInput {
	accountID := connected.ID()
	userID := connected.UserID()
	return application.ImportFromProviderInput{UserID: userID.UUID(), AccountID: accountID.UUID()}
}

func TestImportFromProviderIntegration(t *testing.T) {
	t.Run("should save every imported transaction, the balance and their deliveries together", func(t *testing.T) {
		stack := newStack(t)
		connected := stack.connectedAccount(t)
		useCase := application.NewImportFromProviderUseCase(stack.transactions, stack.accounts, mockprovider.New(), stack.transactionRunner)

		output, err := useCase.Execute(context.Background(), importInput(connected))

		if err != nil {
			t.Fatalf("import: %v", err)
		}
		accountID := connected.ID()
		if got := stack.count(t, "SELECT count(*) FROM transactions WHERE account_id = $1", accountID.UUID()); got != len(output.TransactionIDs) || got == 0 {
			t.Errorf("transactions = %d, want %d", got, len(output.TransactionIDs))
		}
		if got := stack.count(t, "SELECT count(*) FROM accounts WHERE id = $1 AND balance_amount = $2", accountID.UUID(), output.Balance); got != 1 {
			t.Error("the provider balance was not applied")
		}
		imported := stack.count(t, "SELECT count(*) FROM river_job WHERE args->>'event_name' = $1", transaction.EventTypeImported)
		if imported != len(output.TransactionIDs) {
			t.Errorf("imported deliveries = %d, want %d", imported, len(output.TransactionIDs))
		}
		if got := stack.count(t, "SELECT count(*) FROM river_job WHERE args->>'event_name' = $1", account.EventTypeBalanceUpdated); got != 1 {
			t.Errorf("balance deliveries = %d, want 1", got)
		}
	})

	t.Run("should leave no transaction, no delivery and the old balance when a save fails halfway", func(t *testing.T) {
		stack := newStack(t)
		connected := stack.connectedAccount(t)
		transactions := &failingOnSave{Repository: stack.transactions, failOn: 3}
		useCase := application.NewImportFromProviderUseCase(transactions, stack.accounts, mockprovider.New(), stack.transactionRunner)

		_, err := useCase.Execute(context.Background(), importInput(connected))

		if !errors.Is(err, errSaveFailed) {
			t.Fatalf("error = %v, want %v", err, errSaveFailed)
		}
		if transactions.saves != 3 {
			t.Fatalf("saves attempted = %d, want 3 so two transactions were written before the failure", transactions.saves)
		}
		if got := stack.count(t, "SELECT count(*) FROM transactions"); got != 0 {
			t.Errorf("transactions = %d, want 0", got)
		}
		if got := stack.count(t, "SELECT count(*) FROM river_job"); got != 0 {
			t.Errorf("deliveries = %d, want 0", got)
		}
		accountID := connected.ID()
		if got := stack.count(t, "SELECT count(*) FROM accounts WHERE id = $1 AND balance_amount = 0", accountID.UUID()); got != 1 {
			t.Error("the balance changed although the import failed")
		}
	})
}

func TestCrossContextTransactionIntegration(t *testing.T) {
	t.Run("should commit writes from both bounded contexts in one transaction", func(t *testing.T) {
		stack := newStack(t)
		connected := stack.connectedAccount(t)
		user := registeredUser(t)
		if err := connected.Rename("Renamed Together"); err != nil {
			t.Fatalf("rename: %v", err)
		}

		err := stack.transactionRunner.Begin(context.Background(), func(ctx context.Context, transactionContext kernel.TransactionContext) error {
			transactionContext.RecordEventsOf(user)
			transactionContext.RecordEventsOf(connected)
			if err := stack.users.Save(ctx, user); err != nil {
				return err
			}
			return stack.accounts.Save(ctx, connected)
		})

		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if got := stack.count(t, "SELECT count(*) FROM users"); got != 1 {
			t.Errorf("users = %d, want 1", got)
		}
		if got := stack.count(t, "SELECT count(*) FROM accounts WHERE name = 'Renamed Together'"); got != 1 {
			t.Error("the account rename was not committed")
		}
		if got := stack.count(t, "SELECT count(*) FROM river_job WHERE args->>'event_name' = $1", identity.EventTypeRegistered); got != 1 {
			t.Errorf("registered deliveries = %d, want 1", got)
		}
	})

	t.Run("should roll back writes from both bounded contexts together", func(t *testing.T) {
		stack := newStack(t)
		connected := stack.connectedAccount(t)
		user := registeredUser(t)
		if err := connected.Rename("Renamed Together"); err != nil {
			t.Fatalf("rename: %v", err)
		}

		err := stack.transactionRunner.Begin(context.Background(), func(ctx context.Context, transactionContext kernel.TransactionContext) error {
			transactionContext.RecordEventsOf(user)
			if err := stack.users.Save(ctx, user); err != nil {
				return err
			}
			if err := stack.accounts.Save(ctx, connected); err != nil {
				return err
			}
			return errSaveFailed
		})

		if !errors.Is(err, errSaveFailed) {
			t.Fatalf("error = %v, want %v", err, errSaveFailed)
		}
		if got := stack.count(t, "SELECT count(*) FROM users"); got != 0 {
			t.Errorf("users = %d, want 0", got)
		}
		if got := stack.count(t, "SELECT count(*) FROM accounts WHERE name = 'Renamed Together'"); got != 0 {
			t.Error("the account rename survived the rollback")
		}
		if got := stack.count(t, "SELECT count(*) FROM river_job"); got != 0 {
			t.Errorf("deliveries = %d, want 0", got)
		}
		if len(user.Events()) != 1 {
			t.Errorf("pending events = %d, want the registered event kept after the rollback", len(user.Events()))
		}
	})
}

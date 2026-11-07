//go:build bench

package persistencebench_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/platform/db"
)

// The experiment is split into ROUNDS: instead of running every operation of
// one layer before moving to the next, each layer runs a slice and the loop
// repeats. That dilutes environment drift (thermal throttling, Postgres
// autovacuum, another process on the machine) that would always punish
// whichever layer ran last.
const (
	rounds           = 3
	readsPerRound    = 700
	writesPerRound   = 400
	heavyPerRound    = 200
	concurrentOps    = 3000
	concurrentUsers  = 16
	rulesPerCategory = 5
	warmUpReads      = 50
	baselineLayer    = "pgxpool"
)

// accountStore, transactionStore and categoryStore are sized to what the
// benchmark uses (ISP): it measures Save and FindByID, so the reference
// implementations are not forced to write listing SQL for a contract the
// benchmark never exercises. The production adapters still satisfy the full
// domain ports through their own `var _ account.Repository` assertions.
type accountStore interface {
	Save(ctx context.Context, target *account.Account) error
	FindByID(ctx context.Context, id account.AccountID) (*account.Account, error)
}

type transactionStore interface {
	Save(ctx context.Context, target *transaction.Transaction) error
	FindByID(ctx context.Context, id transaction.TransactionID) (*transaction.Transaction, error)
}

type categoryStore interface {
	Save(ctx context.Context, target *category.Category) error
	FindByID(ctx context.Context, id category.CategoryID) (*category.Category, error)
}

type implementation struct {
	name         string
	accounts     accountStore
	transactions transactionStore
	categories   categoryStore
}

type fixtures struct {
	account     *account.Account
	transaction *transaction.Transaction
	category    *category.Category
}

// workload is one measured operation. run executes a SINGLE operation and the
// harness times each call on its own, because per-operation latency is what
// matters, not the total time.
type workload struct {
	name           string
	operationsEach int
	run            func(ctx context.Context, target implementation, prepared fixtures, index int) error
}

func benchmarkImplementations(t *testing.T) []implementation {
	t.Helper()

	return []implementation{
		{
			name:         baselineLayer,
			accounts:     &pgxAccountRepository{pool: benchPool},
			transactions: &pgxTransactionRepository{pool: benchPool},
			categories:   &pgxCategoryRepository{pool: benchPool},
		},
		{
			name:         "sqldb",
			accounts:     &sqlAccountRepository{database: benchDB},
			transactions: &sqlTransactionRepository{database: benchDB},
			categories:   &sqlCategoryRepository{database: benchDB},
		},
		{
			name:         "ent",
			accounts:     entrepo.NewAccountRepository(benchClient),
			transactions: entrepo.NewTransactionRepository(benchClient),
			categories:   entrepo.NewCategoryRepository(benchClient, db.NewUnitOfWork(benchDB)),
		},
	}
}

// benchmarkWorkloads lists the measured operations. category_save_5rules is
// the heaviest (database transaction + upsert + delete + multi-row insert of
// the rules), where the cost of building dynamic queries shows up if it ever
// does. category_find_5rules reads a parent with children: eager loading
// (WithRules) in Ent, two queries in the raw versions, semantically the same.
func benchmarkWorkloads(t *testing.T, ownerID uuid.UUID) []workload {
	return []workload{
		{
			name:           "account_insert",
			operationsEach: writesPerRound,
			run: func(ctx context.Context, target implementation, _ fixtures, _ int) error {
				return target.accounts.Save(ctx, newAccount(t, ownerID))
			},
		},
		{
			name:           "account_upsert",
			operationsEach: writesPerRound,
			run: func(ctx context.Context, target implementation, prepared fixtures, _ int) error {
				return target.accounts.Save(ctx, prepared.account)
			},
		},
		{
			name:           "account_find",
			operationsEach: readsPerRound,
			run: func(ctx context.Context, target implementation, prepared fixtures, _ int) error {
				_, err := target.accounts.FindByID(ctx, prepared.account.ID())
				return err
			},
		},
		{
			name:           "transaction_insert",
			operationsEach: writesPerRound,
			run: func(ctx context.Context, target implementation, prepared fixtures, _ int) error {
				return target.transactions.Save(ctx, newTransaction(t, ownerID, prepared.account.ID()))
			},
		},
		{
			name:           "transaction_find",
			operationsEach: readsPerRound,
			run: func(ctx context.Context, target implementation, prepared fixtures, _ int) error {
				_, err := target.transactions.FindByID(ctx, prepared.transaction.ID())
				return err
			},
		},
		{
			name:           "category_save_5rules",
			operationsEach: heavyPerRound,
			run: func(ctx context.Context, target implementation, prepared fixtures, _ int) error {
				return target.categories.Save(ctx, prepared.category)
			},
		},
		{
			name:           "category_find_5rules",
			operationsEach: readsPerRound,
			run: func(ctx context.Context, target implementation, prepared fixtures, _ int) error {
				_, err := target.categories.FindByID(ctx, prepared.category.ID())
				return err
			},
		},
	}
}

func TestPersistenceLatency(t *testing.T) {
	t.Run("should report sequential and concurrent latency for every persistence layer", func(t *testing.T) {
		ctx := context.Background()
		ownerID := uuid.New()
		implementations := benchmarkImplementations(t)
		workloads := benchmarkWorkloads(t, ownerID)

		prepared := make(map[string]fixtures, len(implementations))
		samples := make(map[string]map[string]*sample, len(implementations))

		for _, target := range implementations {
			prepared[target.name] = prepareFixtures(ctx, t, target, ownerID)
			samples[target.name] = newWorkloadSamples(workloads)
			warmUp(ctx, t, target, prepared[target.name])
		}

		for round := 1; round <= rounds; round++ {
			runRound(ctx, t, implementations, workloads, prepared, samples)
			t.Logf("round %d/%d done", round, rounds)
		}

		reportSequential(t, implementations, workloads, samples)
		reportConcurrent(ctx, t, implementations, prepared)
	})
}

func newWorkloadSamples(workloads []workload) map[string]*sample {
	samples := make(map[string]*sample, len(workloads))
	for _, currentWorkload := range workloads {
		samples[currentWorkload.name] = newSample(currentWorkload.operationsEach * rounds)
	}
	return samples
}

func runRound(ctx context.Context, t *testing.T, implementations []implementation, workloads []workload, prepared map[string]fixtures, samples map[string]map[string]*sample) {
	t.Helper()

	for _, currentWorkload := range workloads {
		for _, target := range implementations {
			runWorkload(ctx, t, target, currentWorkload, prepared[target.name], samples[target.name][currentWorkload.name])
		}
	}
}

func runWorkload(ctx context.Context, t *testing.T, target implementation, current workload, prepared fixtures, collected *sample) {
	t.Helper()

	for index := 0; index < current.operationsEach; index++ {
		start := time.Now()
		err := current.run(ctx, target, prepared, index)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("%s/%s failed on operation %d: %v", target.name, current.name, index, err)
		}
		collected.record(elapsed)
	}
}

// reportSequential prints the comparison table against pgxpool, the thinnest
// layer, so the delta answers "how much does moving up a layer cost".
func reportSequential(t *testing.T, implementations []implementation, workloads []workload, samples map[string]map[string]*sample) {
	t.Helper()

	t.Log("")
	t.Log("=== SEQUENTIAL LATENCY (per operation) ===")
	t.Logf("%-22s %-9s %8s %9s %9s %9s %9s   %s", "workload", "layer", "ops", "p50", "p95", "p99", "mean", "Δp50 vs pgxpool")

	for _, current := range workloads {
		baselineStats := samples[baselineLayer][current.name].stats()

		for _, target := range implementations {
			currentStats := samples[target.name][current.name].stats()
			t.Logf("%-22s %-9s %8d %9s %9s %9s %9s   %s",
				current.name, target.name, currentStats.count,
				round3(currentStats.p50), round3(currentStats.p95), round3(currentStats.p99), round3(currentStats.mean),
				deltaVersus(baselineStats.p50, currentStats.p50))
		}
	}
}

// reportConcurrent measures the hottest read under concurrency, which is where
// differences in pooling and per-operation allocation show up; the sequential
// run leaves the machine idle waiting on the database.
func reportConcurrent(ctx context.Context, t *testing.T, implementations []implementation, prepared map[string]fixtures) {
	t.Helper()

	t.Log("")
	t.Logf("=== CONCURRENT READS (%d goroutines, %d operations) ===", concurrentUsers, concurrentOps)
	t.Logf("%-9s %9s %9s %9s %12s", "layer", "p50", "p95", "p99", "ops/s")

	for _, target := range implementations {
		collected, totalElapsed := readConcurrently(ctx, t, target, prepared[target.name].account.ID())
		concurrentStats := collected.stats()
		throughput := float64(concurrentStats.count) / totalElapsed.Seconds()

		t.Logf("%-9s %9s %9s %9s %12.0f", target.name,
			round3(concurrentStats.p50), round3(concurrentStats.p95), round3(concurrentStats.p99), throughput)
	}
}

func readConcurrently(ctx context.Context, t *testing.T, target implementation, accountID account.AccountID) (*sample, time.Duration) {
	collected := newSample(concurrentOps)
	var mutex sync.Mutex
	var waitGroup sync.WaitGroup
	perUser := concurrentOps / concurrentUsers
	start := time.Now()

	for user := 0; user < concurrentUsers; user++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			local := timeReads(ctx, t, target, accountID, perUser)

			mutex.Lock()
			defer mutex.Unlock()
			for _, duration := range local {
				collected.record(duration)
			}
		}()
	}

	waitGroup.Wait()
	return collected, time.Since(start)
}

// timeReads collects durations locally and lets the caller merge them once at
// the end: locking a mutex per operation would measure the harness's own
// contention instead of the repository's.
func timeReads(ctx context.Context, t *testing.T, target implementation, accountID account.AccountID, reads int) []time.Duration {
	local := make([]time.Duration, 0, reads)
	for index := 0; index < reads; index++ {
		operationStart := time.Now()
		if _, err := target.accounts.FindByID(ctx, accountID); err != nil {
			t.Errorf("%s: concurrent read failed: %v", target.name, err)
			return local
		}
		local = append(local, time.Since(operationStart))
	}
	return local
}

func deltaVersus(baseline time.Duration, current time.Duration) string {
	if baseline == 0 {
		return "-"
	}
	difference := (float64(current) - float64(baseline)) / float64(baseline) * 100
	if difference >= 0 {
		return fmt.Sprintf("+%.1f%%", difference)
	}
	return fmt.Sprintf("%.1f%%", difference)
}

// round3 rounds to the microsecond: nanoseconds in a database round-trip
// measurement are noise, not information.
func round3(duration time.Duration) string {
	return duration.Round(time.Microsecond).String()
}

func prepareFixtures(ctx context.Context, t *testing.T, target implementation, ownerID uuid.UUID) fixtures {
	t.Helper()

	preparedAccount := newAccount(t, ownerID)
	if err := target.accounts.Save(ctx, preparedAccount); err != nil {
		t.Fatalf("%s: failed to prepare the account: %v", target.name, err)
	}

	preparedTransaction := newTransaction(t, ownerID, preparedAccount.ID())
	if err := target.transactions.Save(ctx, preparedTransaction); err != nil {
		t.Fatalf("%s: failed to prepare the transaction: %v", target.name, err)
	}

	preparedCategory := newCategory(t, ownerID)
	if err := target.categories.Save(ctx, preparedCategory); err != nil {
		t.Fatalf("%s: failed to prepare the category: %v", target.name, err)
	}

	return fixtures{account: preparedAccount, transaction: preparedTransaction, category: preparedCategory}
}

// warmUp keeps startup out of the measurement: the first query prepares the
// statement on the server, the first connection does the handshake and Ent
// initializes its metadata.
func warmUp(ctx context.Context, t *testing.T, target implementation, prepared fixtures) {
	t.Helper()

	for index := 0; index < warmUpReads; index++ {
		if _, err := target.accounts.FindByID(ctx, prepared.account.ID()); err != nil {
			t.Fatalf("%s: warm-up failed: %v", target.name, err)
		}
		if _, err := target.transactions.FindByID(ctx, prepared.transaction.ID()); err != nil {
			t.Fatalf("%s: warm-up failed: %v", target.name, err)
		}
		if _, err := target.categories.FindByID(ctx, prepared.category.ID()); err != nil {
			t.Fatalf("%s: warm-up failed: %v", target.name, err)
		}
	}
}

func mustBenchmarkUserID(t *testing.T, ownerID uuid.UUID) shared.UserID {
	t.Helper()

	userID, err := shared.NewUserID(ownerID)
	if err != nil {
		t.Fatalf("invalid user id: %v", err)
	}
	return userID
}

func mustBenchmarkCurrency(t *testing.T) shared.Currency {
	t.Helper()

	currency, err := shared.NewCurrency("BRL")
	if err != nil {
		t.Fatalf("invalid currency: %v", err)
	}
	return currency
}

func newAccount(t *testing.T, ownerID uuid.UUID) *account.Account {
	t.Helper()

	created, err := account.New(mustBenchmarkUserID(t, ownerID), "Benchmark Account", account.KindChecking, mustBenchmarkCurrency(t), account.NewManualSource())
	if err != nil {
		t.Fatalf("invalid account: %v", err)
	}
	return created
}

func newTransaction(t *testing.T, ownerID uuid.UUID, accountID account.AccountID) *transaction.Transaction {
	t.Helper()

	money, err := shared.NewMoney(-4550, mustBenchmarkCurrency(t))
	if err != nil {
		t.Fatalf("invalid amount: %v", err)
	}
	created, err := transaction.NewManual(mustBenchmarkUserID(t, ownerID), accountID, money, time.Now().Add(-time.Hour), "Benchmark")
	if err != nil {
		t.Fatalf("invalid transaction: %v", err)
	}
	return created
}

func newCategory(t *testing.T, ownerID uuid.UUID) *category.Category {
	t.Helper()

	created, err := category.New(mustBenchmarkUserID(t, ownerID), "Benchmark Category", nil)
	if err != nil {
		t.Fatalf("invalid category: %v", err)
	}
	for index := 0; index < rulesPerCategory; index++ {
		addBenchmarkRule(t, created, fmt.Sprintf("keyword-%d", index))
	}
	return created
}

func addBenchmarkRule(t *testing.T, target *category.Category, keyword string) {
	t.Helper()

	rule, err := category.NewCategoryRule(keyword)
	if err != nil {
		t.Fatalf("invalid rule: %v", err)
	}
	if err := target.AddRule(rule); err != nil {
		t.Fatalf("failed to add the rule: %v", err)
	}
}

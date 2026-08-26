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
)

// Tamanhos do experimento. Divididos em RODADAS: em vez de rodar 1500
// operações de uma camada e só depois as da próxima, cada camada roda
// 500 e a volta se repete 3 vezes. Isso dilui deriva do ambiente (throttle
// térmico, autovacuum do Postgres, outro processo na máquina) que castigaria
// sempre quem rodasse por último.
const (
	rounds           = 3
	readsPerRound    = 700
	writesPerRound   = 400
	heavyPerRound    = 200
	concurrentOps    = 3000
	concurrentUsers  = 16
	rulesPerCategory = 5
)

// implementation é uma camada de persistência sob teste. As três satisfazem
// as MESMAS portas do domínio — é isso que permite medi-las com um harness
// único, e é a mesma propriedade que permitiu trocar pgx por Ent sem tocar
// use case.
type implementation struct {
	name         string
	accounts     account.Repository
	transactions transaction.Repository
	categories   category.Repository
}

// fixtures são os aggregates já persistidos, usados pelos workloads de
// leitura e de update.
type fixtures struct {
	account     *account.Account
	transaction *transaction.Transaction
	category    *category.Category
}

// workload é uma operação medida. `run` executa UMA operação; o harness
// cronometra cada chamada individualmente (é a latência por operação que
// interessa, não o tempo total).
type workload struct {
	name           string
	operationsEach int
	run            func(ctx context.Context, target implementation, prepared fixtures, index int) error
}

func TestPersistenceLatency(t *testing.T) {
	ctx := context.Background()
	ownerID := uuid.New()

	implementations := []implementation{
		{
			name:         "pgxpool",
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
			categories:   entrepo.NewCategoryRepository(benchClient),
		},
	}

	workloads := []workload{
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
			// O workload mais pesado: transação de banco + upsert + delete +
			// insert multi-linha de 5 regras. É onde o custo de montar
			// query dinâmica aparece, se aparecer.
			name:           "category_save_5rules",
			operationsEach: heavyPerRound,
			run: func(ctx context.Context, target implementation, prepared fixtures, _ int) error {
				return target.categories.Save(ctx, prepared.category)
			},
		},
		{
			// Leitura com filho: no Ent é eager loading (WithRules); nas
			// versões cruas são duas queries. Semanticamente igual.
			name:           "category_find_5rules",
			operationsEach: readsPerRound,
			run: func(ctx context.Context, target implementation, prepared fixtures, _ int) error {
				_, err := target.categories.FindByID(ctx, prepared.category.ID())
				return err
			},
		},
	}

	prepared := make(map[string]fixtures, len(implementations))
	samples := make(map[string]map[string]*sample, len(implementations))

	for _, target := range implementations {
		prepared[target.name] = prepareFixtures(ctx, t, target, ownerID)
		samples[target.name] = make(map[string]*sample, len(workloads))
		for _, currentWorkload := range workloads {
			samples[target.name][currentWorkload.name] = newSample(currentWorkload.operationsEach * rounds)
		}
		// Aquecimento: primeira query prepara statement no servidor, primeira
		// conexão faz handshake, e o Ent inicializa metadata. Medir isso
		// junto mediria startup, não latência de operação.
		warmUp(ctx, t, target, prepared[target.name])
	}

	for round := 1; round <= rounds; round++ {
		for _, currentWorkload := range workloads {
			for _, target := range implementations {
				runWorkload(ctx, t, target, currentWorkload, prepared[target.name], samples[target.name][currentWorkload.name])
			}
		}
		t.Logf("rodada %d/%d concluída", round, rounds)
	}

	reportSequential(t, implementations, workloads, samples)
	reportConcurrent(ctx, t, implementations, prepared)
}

// runWorkload executa e cronometra uma célula da matriz.
func runWorkload(ctx context.Context, t *testing.T, target implementation, current workload, prepared fixtures, collected *sample) {
	t.Helper()

	for index := 0; index < current.operationsEach; index++ {
		start := time.Now()
		err := current.run(ctx, target, prepared, index)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("%s/%s falhou na operação %d: %v", target.name, current.name, index, err)
		}
		collected.record(elapsed)
	}
}

// reportSequential imprime a tabela comparativa. O baseline é pgxpool (a
// camada mais fina), então o delta responde "quanto custa subir de camada".
func reportSequential(t *testing.T, implementations []implementation, workloads []workload, samples map[string]map[string]*sample) {
	t.Helper()

	const baseline = "pgxpool"

	t.Log("")
	t.Log("=== LATÊNCIA SEQUENCIAL (por operação) ===")
	t.Logf("%-22s %-9s %8s %9s %9s %9s %9s   %s", "workload", "camada", "ops", "p50", "p95", "p99", "média", "Δp50 vs pgxpool")

	for _, current := range workloads {
		baselineStats := samples[baseline][current.name].stats()

		for _, target := range implementations {
			currentStats := samples[target.name][current.name].stats()
			t.Logf("%-22s %-9s %8d %9s %9s %9s %9s   %s",
				current.name, target.name, currentStats.count,
				round3(currentStats.p50), round3(currentStats.p95), round3(currentStats.p99), round3(currentStats.mean),
				deltaVersus(baselineStats.p50, currentStats.p50))
		}
	}
}

// reportConcurrent mede a leitura mais quente sob concorrência: é onde
// diferença de pool e de alocação por operação aparece, e não na medição
// sequencial (onde a máquina está ociosa esperando o banco).
func reportConcurrent(ctx context.Context, t *testing.T, implementations []implementation, prepared map[string]fixtures) {
	t.Helper()

	t.Log("")
	t.Logf("=== LEITURA CONCORRENTE (%d goroutines, %d operações) ===", concurrentUsers, concurrentOps)
	t.Logf("%-9s %9s %9s %9s %12s", "camada", "p50", "p95", "p99", "ops/s")

	for _, target := range implementations {
		collected := newSample(concurrentOps)
		var mutex sync.Mutex
		var waitGroup sync.WaitGroup

		accountID := prepared[target.name].account.ID()
		perUser := concurrentOps / concurrentUsers
		start := time.Now()

		for user := 0; user < concurrentUsers; user++ {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()

				// Cada goroutine acumula localmente e entrega no fim: travar
				// o mutex por operação mediria a contenção do PRÓPRIO
				// harness em vez da do repositório.
				local := make([]time.Duration, 0, perUser)
				for index := 0; index < perUser; index++ {
					operationStart := time.Now()
					if _, err := target.accounts.FindByID(ctx, accountID); err != nil {
						t.Errorf("%s: leitura concorrente falhou: %v", target.name, err)
						return
					}
					local = append(local, time.Since(operationStart))
				}

				mutex.Lock()
				defer mutex.Unlock()
				for _, duration := range local {
					collected.record(duration)
				}
			}()
		}

		waitGroup.Wait()
		totalElapsed := time.Since(start)
		concurrentStats := collected.stats()
		throughput := float64(concurrentStats.count) / totalElapsed.Seconds()

		t.Logf("%-9s %9s %9s %9s %12.0f", target.name,
			round3(concurrentStats.p50), round3(concurrentStats.p95), round3(concurrentStats.p99), throughput)
	}
}

// deltaVersus formata a diferença relativa entre dois percentis.
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

// round3 corta a precisão pra tabela ficar legível — nanossegundo em
// medição de round-trip de banco é ruído, não informação.
func round3(duration time.Duration) string {
	return duration.Round(time.Microsecond).String()
}

// --- Fixtures ------------------------------------------------------------

func prepareFixtures(ctx context.Context, t *testing.T, target implementation, ownerID uuid.UUID) fixtures {
	t.Helper()

	preparedAccount := newAccount(t, ownerID)
	if err := target.accounts.Save(ctx, preparedAccount); err != nil {
		t.Fatalf("%s: falha preparando conta: %v", target.name, err)
	}

	preparedTransaction := newTransaction(t, ownerID, preparedAccount.ID())
	if err := target.transactions.Save(ctx, preparedTransaction); err != nil {
		t.Fatalf("%s: falha preparando transação: %v", target.name, err)
	}

	preparedCategory := newCategory(t, ownerID)
	if err := target.categories.Save(ctx, preparedCategory); err != nil {
		t.Fatalf("%s: falha preparando categoria: %v", target.name, err)
	}

	return fixtures{account: preparedAccount, transaction: preparedTransaction, category: preparedCategory}
}

func warmUp(ctx context.Context, t *testing.T, target implementation, prepared fixtures) {
	t.Helper()

	for index := 0; index < 50; index++ {
		if _, err := target.accounts.FindByID(ctx, prepared.account.ID()); err != nil {
			t.Fatalf("%s: aquecimento falhou: %v", target.name, err)
		}
		if _, err := target.transactions.FindByID(ctx, prepared.transaction.ID()); err != nil {
			t.Fatalf("%s: aquecimento falhou: %v", target.name, err)
		}
		if _, err := target.categories.FindByID(ctx, prepared.category.ID()); err != nil {
			t.Fatalf("%s: aquecimento falhou: %v", target.name, err)
		}
	}
}

func newAccount(t *testing.T, ownerID uuid.UUID) *account.Account {
	t.Helper()

	userID, err := shared.NewUserID(ownerID)
	if err != nil {
		t.Fatalf("userID inválido: %v", err)
	}
	currency, err := shared.NewCurrency("BRL")
	if err != nil {
		t.Fatalf("moeda inválida: %v", err)
	}
	created, err := account.New(userID, "Conta de Benchmark", account.KindChecking, currency, account.NewManualSource())
	if err != nil {
		t.Fatalf("conta inválida: %v", err)
	}
	return created
}

func newTransaction(t *testing.T, ownerID uuid.UUID, accountID account.AccountID) *transaction.Transaction {
	t.Helper()

	userID, err := shared.NewUserID(ownerID)
	if err != nil {
		t.Fatalf("userID inválido: %v", err)
	}
	currency, err := shared.NewCurrency("BRL")
	if err != nil {
		t.Fatalf("moeda inválida: %v", err)
	}
	money, err := shared.NewMoney(-4550, currency)
	if err != nil {
		t.Fatalf("quantia inválida: %v", err)
	}
	created, err := transaction.NewManual(userID, accountID, money, time.Now().Add(-time.Hour), "Benchmark")
	if err != nil {
		t.Fatalf("transação inválida: %v", err)
	}
	return created
}

func newCategory(t *testing.T, ownerID uuid.UUID) *category.Category {
	t.Helper()

	userID, err := shared.NewUserID(ownerID)
	if err != nil {
		t.Fatalf("userID inválido: %v", err)
	}
	created, err := category.New(userID, "Categoria de Benchmark", nil)
	if err != nil {
		t.Fatalf("categoria inválida: %v", err)
	}
	for index := 0; index < rulesPerCategory; index++ {
		rule, err := category.NewCategoryRule(fmt.Sprintf("keyword-%d", index))
		if err != nil {
			t.Fatalf("regra inválida: %v", err)
		}
		if err := created.AddRule(rule); err != nil {
			t.Fatalf("falha adicionando regra: %v", err)
		}
	}
	return created
}

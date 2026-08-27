//go:build bench

package persistencebench_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// newBenchmarkCurrency e newBenchmarkUserID existem porque os builders dos
// testes de latência recebem *testing.T, e benchmark usa *testing.B.
func newBenchmarkCurrency() (shared.Currency, error) {
	return shared.NewCurrency("BRL")
}

func newBenchmarkUserID(raw uuid.UUID) (shared.UserID, error) {
	return shared.NewUserID(raw)
}

// Os benchmarks abaixo medem a dimensão que a tabela de latência NÃO
// mostra: quanto de MEMÓRIA e quantas alocações cada camada gasta por
// operação.
//
// Por que isso importa mais que a latência: a latência de uma operação de
// banco é dominada pelo round-trip, então a camada Go quase desaparece na
// média. Alocação, não — ela é toda da camada, escala com a taxa de
// requisições e reaparece depois como pressão de GC (que aí sim vira
// latência, na cauda, no pior momento).
//
// Rodar com: go test -tags bench -bench BenchmarkFind -benchmem -run '^$'

// benchmarkFind mede a leitura mais quente do sistema. Recebe a interface
// mínima (accountStore), não a porta completa: o benchmark mede FindByID e
// não tem por que exigir os métodos de listagem.
func benchmarkFind(b *testing.B, repository accountStore, accountID account.AccountID) {
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		if _, err := repository.FindByID(ctx, accountID); err != nil {
			b.Fatalf("leitura falhou: %v", err)
		}
	}
}

// prepareBenchmarkAccount grava uma conta e devolve o id.
func prepareBenchmarkAccount(b *testing.B) account.AccountID {
	b.Helper()

	ctx := context.Background()
	userID := uuid.New()

	currency, err := newBenchmarkCurrency()
	if err != nil {
		b.Fatalf("moeda inválida: %v", err)
	}
	domainUserID, err := newBenchmarkUserID(userID)
	if err != nil {
		b.Fatalf("userID inválido: %v", err)
	}
	created, err := account.New(domainUserID, "Conta de Benchmark", account.KindChecking, currency, account.NewManualSource())
	if err != nil {
		b.Fatalf("conta inválida: %v", err)
	}

	repository := entrepo.NewAccountRepository(benchClient)
	if err := repository.Save(ctx, created); err != nil {
		b.Fatalf("falha preparando conta: %v", err)
	}
	return created.ID()
}

func BenchmarkFindPgxpool(b *testing.B) {
	accountID := prepareBenchmarkAccount(b)
	benchmarkFind(b, &pgxAccountRepository{pool: benchPool}, accountID)
}

func BenchmarkFindSQLDB(b *testing.B) {
	accountID := prepareBenchmarkAccount(b)
	benchmarkFind(b, &sqlAccountRepository{database: benchDB}, accountID)
}

func BenchmarkFindEnt(b *testing.B) {
	accountID := prepareBenchmarkAccount(b)
	benchmarkFind(b, entrepo.NewAccountRepository(benchClient), accountID)
}

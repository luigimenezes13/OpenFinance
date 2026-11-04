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

// benchmarkFind measures the memory and allocations each layer spends on the
// hottest read, which the latency table does not show. Latency of a database
// operation is dominated by the round-trip, so the Go layer almost vanishes in
// the mean; allocation belongs entirely to the layer, scales with request rate
// and comes back as GC pressure in the tail. It takes the minimal accountStore
// instead of the full port because only FindByID is measured.
//
// Run with: go test -tags bench -bench BenchmarkFind -benchmem -run '^$'
func benchmarkFind(b *testing.B, repository accountStore, accountID account.AccountID) {
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for index := 0; index < b.N; index++ {
		if _, err := repository.FindByID(ctx, accountID); err != nil {
			b.Fatalf("read failed: %v", err)
		}
	}
}

func prepareBenchmarkAccount(b *testing.B) account.AccountID {
	b.Helper()

	currency, err := shared.NewCurrency("BRL")
	if err != nil {
		b.Fatalf("invalid currency: %v", err)
	}
	userID, err := shared.NewUserID(uuid.New())
	if err != nil {
		b.Fatalf("invalid user id: %v", err)
	}
	created, err := account.New(userID, "Benchmark Account", account.KindChecking, currency, account.NewManualSource())
	if err != nil {
		b.Fatalf("invalid account: %v", err)
	}

	repository := entrepo.NewAccountRepository(benchClient, newUnsubscribedWriter(b))
	if err := repository.Save(context.Background(), created); err != nil {
		b.Fatalf("failed to prepare the account: %v", err)
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
	benchmarkFind(b, entrepo.NewAccountRepository(benchClient, newUnsubscribedWriter(b)), accountID)
}

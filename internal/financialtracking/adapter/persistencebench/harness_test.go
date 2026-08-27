//go:build bench

// Package persistencebench_test mede LATÊNCIA das três camadas de
// persistência contra o mesmo Postgres, com o mesmo SQL semântico:
//
//	ent     — client gerado pelo Ent, sobre database/sql + pgx stdlib
//	sqldb   — SQL escrito à mão, sobre database/sql + pgx stdlib
//	pgxpool — SQL escrito à mão, sobre a API nativa do pgx
//
// A terceira variante existe pra separar o custo do ORM do custo da camada
// database/sql. Comparar só ent vs pgxpool atribuiria ao "ORM" uma
// diferença que é em parte do database/sql.
//
// HONESTIDADE SOBRE O QUE ISSO MEDE: o Postgres está em Docker no
// localhost, então cada operação é dominada pelo round-trip local + tempo
// de banco. A diferença entre as camadas é o que sobra depois disso — o
// número absoluto não se transporta pra produção (onde a rede é maior e a
// fração da camada Go fica MENOR ainda), mas a ORDEM entre elas se
// transporta.
//
// Como rodar: make bench-persistence
package persistencebench_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo/ent"
	"github.com/luigimenezes13/financial-manager/internal/platform/db"
)

const migrationsDir = "../../../../migrations/financialtracking"

var (
	benchDB     *sql.DB
	benchPool   *pgxpool.Pool
	benchClient *ent.Client
)

// TestMain prepara UMA vez: schema limpo, migrations aplicadas, as três
// conexões abertas.
func TestMain(main *testing.M) {
	databaseURL := os.Getenv("BENCH_DATABASE_URL")
	if databaseURL == "" {
		fmt.Println("BENCH_DATABASE_URL vazio — use `make bench-persistence`")
		os.Exit(1)
	}

	ctx := context.Background()

	database, err := db.NewSQLDB(ctx, databaseURL)
	if err != nil {
		fmt.Printf("falha conectando (database/sql): %v\n", err)
		os.Exit(1)
	}
	benchDB = database
	benchClient = entrepo.NewClient(database)

	// O pool nativo recebe os MESMOS limites do platform/db, senão a
	// comparação mediria configuração de pool em vez de camada.
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		fmt.Printf("url inválida pro pgxpool: %v\n", err)
		os.Exit(1)
	}
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 5
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		fmt.Printf("falha conectando (pgxpool): %v\n", err)
		os.Exit(1)
	}
	benchPool = pool

	if err := prepareSchema(ctx, database); err != nil {
		fmt.Printf("falha preparando schema: %v\n", err)
		os.Exit(1)
	}

	code := main.Run()

	pool.Close()
	_ = benchClient.Close()
	os.Exit(code)
}

// prepareSchema derruba e recria o schema com as migrations versionadas.
func prepareSchema(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		return err
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	for _, file := range files {
		statements, err := os.ReadFile(filepath.Join(migrationsDir, file))
		if err != nil {
			return err
		}
		if _, err := database.ExecContext(ctx, string(statements)); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	}
	return nil
}

// --- Estatística ---------------------------------------------------------

// sample acumula as durações de uma célula (implementação × workload).
type sample struct {
	durations []time.Duration
}

func newSample(capacity int) *sample {
	return &sample{durations: make([]time.Duration, 0, capacity)}
}

func (s *sample) record(elapsed time.Duration) {
	s.durations = append(s.durations, elapsed)
}

// stats calcula as estatísticas ORDENANDO uma cópia — ordenar o slice
// original mudaria a ordem de chegada, que é informação (por exemplo, pra
// enxergar aquecimento).
func (s *sample) stats() statistics {
	if len(s.durations) == 0 {
		return statistics{}
	}

	sorted := make([]time.Duration, len(s.durations))
	copy(sorted, s.durations)
	sort.Slice(sorted, func(first, second int) bool { return sorted[first] < sorted[second] })

	var total time.Duration
	for _, duration := range sorted {
		total += duration
	}

	return statistics{
		count: len(sorted),
		mean:  total / time.Duration(len(sorted)),
		p50:   percentile(sorted, 0.50),
		p95:   percentile(sorted, 0.95),
		p99:   percentile(sorted, 0.99),
		max:   sorted[len(sorted)-1],
	}
}

type statistics struct {
	count int
	mean  time.Duration
	p50   time.Duration
	p95   time.Duration
	p99   time.Duration
	max   time.Duration
}

// percentile usa o método do índice mais próximo (nearest-rank): sem
// interpolação, o valor devolvido é uma medição REAL e não uma média entre
// duas — o que importa quando se discute cauda.
func percentile(sorted []time.Duration, fraction float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * fraction)
	return sorted[index]
}

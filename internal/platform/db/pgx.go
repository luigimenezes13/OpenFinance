// Package db é infraestrutura de persistência compartilhada: abre e
// configura a conexão. Vive em platform/ porque não é bounded context — é
// encanamento que os adapters de qualquer BC usam.
//
// Nada aqui conhece aggregate, VO, use case ou Ent: devolve um *sql.DB
// cru, e quem o transforma em client tipado é o adapter do BC. Por isso o
// package não importa nada de financialtracking/.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// O driver do pgx registrado no database/sql. Import só por efeito
	// colateral (o `_`): ninguém chama a API dele direto.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// driverName é o nome com que o pgx/v5/stdlib se registra no database/sql.
const driverName = "pgx"

// Limites do pool, explícitos porque os defaults do database/sql são ruins
// em produção: MaxOpenConns default é ILIMITADO (o serviço estoura o
// max_connections do Postgres sob carga) e ConnMaxLifetime default é
// infinito (conexão eterna atravessa failover e fica falando com um
// primário que virou réplica).
const (
	maxOpenConns    = 10
	maxIdleConns    = 5
	connMaxLifetime = time.Hour
	connMaxIdleTime = 5 * time.Minute
)

// NewSQLDB abre a conexão e VERIFICA que o banco responde.
//
// O Ping é o ponto principal: sql.Open não conecta (é lazy), então sem ele
// a função devolveria um *sql.DB "válido" com o banco fora do ar, e o erro
// só apareceria na primeira request — o processo subiria se achando
// saudável.
//
// Por que *sql.DB e não pgxpool: o Ent fala database/sql. Trocamos a API
// nativa do pgx (pool com health check próprio, batch, COPY) pelo client
// tipado gerado — se um dia um caso exigir COPY em massa, o caminho é abrir
// um pgxpool ao lado, não desfazer isto.
func NewSQLDB(ctx context.Context, databaseURL string) (*sql.DB, error) {
	database, err := sql.Open(driverName, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: url inválida: %w", err)
	}

	database.SetMaxOpenConns(maxOpenConns)
	database.SetMaxIdleConns(maxIdleConns)
	database.SetConnMaxLifetime(connMaxLifetime)
	database.SetConnMaxIdleTime(connMaxIdleTime)

	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("db: banco inacessível: %w", err)
	}

	return database, nil
}

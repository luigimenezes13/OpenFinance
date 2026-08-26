# Alvos de desenvolvimento. `make test` é o do dia a dia; integração é
# separada de propósito (precisa de Postgres).

DB_HOST_PORT    ?= 55432
DB_CREDENTIALS  ?= financial:financial@localhost:$(DB_HOST_PORT)
DATABASE_URL    ?= postgres://$(DB_CREDENTIALS)/financial_manager?sslmode=disable
TEST_DB_URL     ?= postgres://$(DB_CREDENTIALS)/financial_manager_test?sslmode=disable
BENCH_DB_URL    ?= postgres://$(DB_CREDENTIALS)/financial_manager_bench?sslmode=disable
# Banco DESCARTÁVEL do Atlas: ele reaplica o histórico de migrations aqui pra
# calcular o diff. Nunca apontar pra produção.
ATLAS_DEV_URL   ?= postgres://$(DB_CREDENTIALS)/financial_manager_dev?sslmode=disable&search_path=public

MIGRATE_MAIN := internal/financialtracking/adapter/entrepo/ent/migrate/main.go
ENT_DIR      := internal/financialtracking/adapter/entrepo/ent

.PHONY: run test test-integration bench-persistence generate migrate-diff migrate-apply db-up db-databases db-down db-reset fmt vet check

# Sobe a API localmente contra o Postgres do compose.
run: db-up
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/api

# Testes unitários: domínio e application. Sem banco, sem rede.
test:
	go test ./... -count=1

# Testes de integração: repositórios contra Postgres real, com as migrations
# geradas aplicadas. A build tag `integration` os mantém fora do `make test`.
test-integration: db-up
	TEST_DATABASE_URL="$(TEST_DB_URL)" go test ./... -count=1 -tags integration -run Integration

# Compara a latência das três camadas de persistência (ent, database/sql
# cru, pgxpool cru) contra o mesmo Postgres. Atrás da tag `bench` porque
# leva minutos e escreve milhares de linhas.
bench-persistence: db-up
	BENCH_DATABASE_URL="$(BENCH_DB_URL)" go test ./internal/financialtracking/adapter/persistencebench/ \
		-count=1 -tags bench -run TestPersistenceLatency -v -timeout 30m

# Regenera o client tipado a partir de ent/schema. Rodar SEMPRE que um
# schema mudar — o código gerado é versionado, não é artefato de build.
generate:
	cd $(ENT_DIR) && go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/versioned-migration ./schema

# Gera a migration do diff entre o schema declarado e o histórico já
# existente — o equivalente do `prisma migrate dev`.
# Uso: make migrate-diff name=add_budget_table
migrate-diff: db-up
	@test -n "$(name)" || (echo "uso: make migrate-diff name=<nome_da_mudanca>"; exit 1)
	ATLAS_DEV_DATABASE_URL="$(ATLAS_DEV_URL)" go run -mod=mod $(MIGRATE_MAIN) $(name)

# DEV: derruba o schema do banco de desenvolvimento e reaplica TODAS as
# migrations. Destrutivo de propósito e só para desenvolvimento — em
# produção quem aplica é o Atlas CLI (`atlas migrate apply`), que controla
# histórico e checa o atlas.sum.
migrate-apply: db-up
	docker compose exec -T postgres psql -U financial -d financial_manager -v ON_ERROR_STOP=1 \
		-c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
	@for file in migrations/*.sql; do \
		echo "aplicando $$file"; \
		docker compose exec -T postgres psql -U financial -d financial_manager -v ON_ERROR_STOP=1 -f - < $$file; \
	done

db-up: db-databases

# Sobe o Postgres e garante os bancos auxiliares. O script de init do compose
# só roda em volume vazio, então aqui os bancos são criados de forma
# idempotente — assim `make test-integration` funciona em volume antigo.
db-databases:
	docker compose up -d --wait postgres
	@for database in financial_manager_test financial_manager_dev financial_manager_bench; do \
		docker compose exec -T postgres psql -U financial -d postgres -tc \
			"SELECT 1 FROM pg_database WHERE datname='$$database'" | grep -q 1 || \
		docker compose exec -T postgres psql -U financial -d postgres -c \
			"CREATE DATABASE $$database"; \
	done

db-down:
	docker compose down

# Apaga o volume junto: usar quando quiser começar do zero.
db-reset:
	docker compose down -v
	$(MAKE) db-up

fmt:
	gofmt -l -w .

vet:
	go vet ./...

# O que rodar antes de commitar.
check: fmt vet test

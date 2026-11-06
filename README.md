# Financial Manager

Gestor de gastos pessoais integrado ao **Open Finance brasileiro**, escrito em **Go**.

## Intuito

Este projeto tem um propósito duplo, e os dois são igualmente importantes:

1. **Produto**: um gestor de finanças pessoais que importa contas e transações automaticamente dos bancos via Open Finance (agregador Pluggy), permite categorizar gastos e, futuramente, acompanhar metas e orçamentos.

2. **Engenharia**: um estudo aprofundado — e público — de como aplicar **Domain-Driven Design**, **Clean Architecture** e **SOLID** em Go idiomático, sem frameworks escondendo as decisões. Cada fatia do sistema é modelada estrategicamente (bounded contexts, aggregates, domain events) antes de virar código, e o código privilegia clareza de domínio sobre atalhos técnicos.

Em outras palavras: o domínio financeiro é real, mas o projeto também existe para demonstrar *como* construir — não apenas *o que* construir.

## Visão estratégica

O sistema é dividido em 4 bounded contexts, cada um com modelo e linguagem próprios:

| Bounded Context | Responsabilidade | Papel |
|---|---|---|
| **Financial Tracking** | Account, Transaction, Category | Núcleo do produto |
| **Open Finance Integration** | Consent, Item, conexões bancárias | Anti-corruption layer do Pluggy |
| **Identity** | Usuário, autenticação | Upstream para os outros contexts |
| **Goals & Budgets** | Metas, orçamentos, alertas | Consome eventos de Financial Tracking |

Os diagramas completos estão em [`docs/`](docs/):

- [Bounded contexts e relações](docs/bounded_contexts_gestor_gastos.svg)
- [Arquitetura geral](docs/arquitetura_gestor_gastos_openfinance.svg)
- [Aggregates do Financial Tracking](docs/agregados_financial_tracking.svg)
- [Aggregate Goal/Budget](docs/agregado_goal_budget.svg)

## Arquitetura

Clean Architecture com a regra de dependência apontando sempre para dentro, validada pelo compilador via `internal/` e estrutura de imports:

```
adapter/  ────→  application/  ────→  domain/
   ↓                  │                  ↑
   └──────────────────┴──────────────────┘
                      ↑
               platform/  (implementa interfaces do domain)
```

Decisões que regem todo o código:

- **Ports vivem no domain** (estilo Evans): repositories, a porta `OpenFinanceProvider` e, no kernel, o `UnitOfWork` e o `Subscriber` são interfaces declaradas dentro do domínio; `application/` contém apenas use cases.
- **Dinheiro é `int64` em centavos** encapsulado no VO `Money`. Float é proibido em domínio financeiro.
- **Sem ORM**: persistência com `pgx` e SQL escrito à mão.
- **Aggregates encapsulados**: campos não-exportados, mutação só por métodos do root, eventos de domínio emitidos apenas pelo aggregate root.
- **Composition root único** (`cmd/api/main.go`): o único lugar do sistema que conhece tipos concretos. Trocar Postgres, provider ou fila do outbox não toca em domínio nem use cases.

O design completo da primeira fatia está em [`docs/superpowers/specs/2026-06-16-financial-tracking-v1-design.md`](docs/superpowers/specs/2026-06-16-financial-tracking-v1-design.md).

## Stack

| Camada | Escolha | Por quê |
|---|---|---|
| Linguagem | Go 1.26 | — |
| HTTP | Gin | Handlers finos; nada abaixo da borda conhece `*gin.Context`. |
| Persistência | PostgreSQL + **Ent** (schema como código, client gerado) | Papel do Prisma no ecossistema Go. As entities do Ent são modelo de persistência, não de domínio: a ponte é o `Snapshot` do aggregate. |
| Migrations | **Atlas** via `ent/migrate` | SQL versionado **gerado** do diff do schema — ninguém escreve `CREATE TABLE`. Forward-only. |
| Autenticação | Google OIDC (ID token) | SSO delegado: nenhuma senha, hash ou reset no nosso lado. |
| Fila do outbox | **River** (`riverdatabasesql`, poll-only) | Lease com `SKIP LOCKED`, retry com backoff, dead letter e shutdown gracioso sobre o mesmo Postgres. Nada de broker para operar. |
| Testes | stdlib `testing` + testify | Unitários, de borda (httptest) e de integração contra Postgres real. |
| Logging | `log/slog` (stdlib) | — |

## Rodando

Requer Docker e Go.

```sh
make db-up            # Postgres na porta 55432 (+ bancos de teste e do Atlas)
make migrate-apply    # aplica as migrations no banco de desenvolvimento
make run              # sobe a API (exige GOOGLE_CLIENT_ID e ID token do Google)
```

Para testar à mão sem frontend, existe uma build com **autenticação falsa**, em que o token é um e-mail:

```sh
make run-devauth
curl localhost:8080/v1/me -H "Authorization: Bearer voce@example.com"
```

Isso é uma **build tag**, não uma variável de ambiente: o binário de produção não contém o código (`go build ./cmd/api` sem `-tags devauth` não compila nem o package), então não existe configuração capaz de ativá-lo em produção.

Tudo em containers, sem Go instalado:

```sh
docker compose --profile api up --build
```

### Variáveis de ambiente

| Variável | Obrigatória | Descrição |
|---|---|---|
| `DATABASE_URL` | sim | Postgres. Sem default — default silencioso de banco é como um serviço de produção acaba conectado na máquina de alguém. |
| `GOOGLE_CLIENT_ID` | sim | Audience esperada nos ID tokens. Sem ela o processo **não sobe**: audience não validada aceitaria token legítimo do Google emitido para outro aplicativo. |
| `CORS_ALLOWED_ORIGINS` | não | Lista separada por vírgula. Vazio = nenhum cabeçalho CORS (default restrito). |
| `HTTP_ADDR` | não | Default `:8080`. |
| `OUTBOX_MAX_WORKERS` | não | Entregas de evento em paralelo neste processo. Default `10`. |
| `OUTBOX_JOB_TIMEOUT` | não | Tempo máximo de uma entrega, no formato `30s` ou `2m`. Default `30s`. |

## Endpoints

Todas as rotas `/v1` exigem `Authorization: Bearer <google_id_token>`.

| Método | Rota | Ação |
|---|---|---|
| GET | `/healthz` | Saúde do processo **com** ping no banco (fora do grupo autenticado) |
| GET | `/v1/me` | Perfil do usuário autenticado |
| POST · GET | `/v1/accounts` | Criar · listar contas |
| GET · PATCH | `/v1/accounts/:id` | Detalhe · renomear |
| POST · GET | `/v1/categories` | Criar · listar categorias |
| PATCH | `/v1/categories/:id` | Renomear |
| PUT | `/v1/categories/:id/parent` | Mover na hierarquia (`parent_id: null` = virar raiz) |
| POST · DELETE | `/v1/categories/:id/rules[/:ruleId]` | Criar · remover regra de auto-categorização |
| POST · GET | `/v1/transactions` | Lançar · extrato (filtros `account_id`, `category_id`, `from`, `to`, `limit`, `offset`) |
| GET | `/v1/transactions/:id` | Detalhe |
| PUT | `/v1/transactions/:id/category` | Categorizar |
| POST | `/v1/transactions/import` | Importar do provedor Open Finance |

## Decisões que valem destacar

- **O saldo entra por um caminho só: a importação Open Finance.** Lançamento manual não move saldo. O banco é a autoridade sobre quanto existe na conta; derivar saldo dos lançamentos digitados produziria um número que discorda do extrato bancário. Há testes de regressão em dois níveis impedindo o "conserto" bem-intencionado.
- **Sem FK entre aggregates.** `accounts`, `transactions` e `categories` são fronteiras de consistência separadas. FK existe apenas *dentro* do aggregate (`category_rules` → `categories`, com CASCADE).
- **Um schema/client/migration Ent por bounded context.** Assim é o compilador que garante a fronteira: o client do Identity não tem entity de `Account` para consultar.
- **O `sub` do Google é referência externa, não identidade do sistema.** Cada BC tem seu próprio `UserID`; a tradução entre contextos é em uuid puro, na borda.
- **Evento de domínio sai por outbox transacional.** O `Save` do repositório grava o aggregate e enfileira uma entrega por assinante na mesma transação; um worker do River, rodando junto da API, entrega depois com retry. Processo que morre entre o commit e a entrega não perde evento, e assinante que falha não derruba a requisição. A entrega é at-least-once: o assinante deduplica pelo `ID` do envelope. Desenho completo no [spec do outbox](docs/superpowers/specs/2026-11-04-outbox-design.md).
- **Importação é tudo ou nada.** O provider é consultado antes de abrir a transação (rede dentro de transação seguraria lock); depois as transações importadas e o saldo são gravados numa unidade só.
- **Erro de domínio é traduzido para HTTP em um lugar só**, por uma lista percorrida com `errors.Is`. 400 é "não entendi a requisição"; 422 é "entendi e o domínio recusou".

As decisões completas, com data e motivo, estão no histórico do [spec de design](docs/superpowers/specs/2026-06-16-financial-tracking-v1-design.md).

### Evento que esgotou as tentativas

Uma entrega que falha 25 vezes vira `discarded` no `river_job` e para de ser tentada. Para ver e reenfileirar:

```sh
psql "$DATABASE_URL" -c "SELECT id, args->>'subscriber', args->>'event_name', errors[array_length(errors, 1)]->>'error' FROM river_job WHERE state = 'discarded'"
psql "$DATABASE_URL" -c "UPDATE river_job SET state = 'available', finalized_at = NULL, scheduled_at = now(), max_attempts = attempt + 25 WHERE id = <id>"
```

Ao subir a versão do River no `go.mod`, extraia as migrations novas com `make migrate-river-get` e grave só a diferença num arquivo novo em `migrations/river/`.

## Estrutura do repositório

```
├── cmd/api/                          # composition root (DI manual)
├── internal/
│   ├── kernel/                       # shared kernel: TypedID, EventRecorder, Page, contrato de eventos
│   ├── identity/                     # BC Identity (SSO Google)
│   │   ├── domain/                   # User, Email, ExternalIdentity, AvatarURL, portas
│   │   ├── application/              # sign-in, perfil
│   │   └── adapter/                  # googleoidc, entrepo, ginmiddleware, ginhandler, devauth
│   ├── financialtracking/            # BC núcleo
│   │   ├── domain/                   # aggregates, VOs, eventos, portas
│   │   ├── application/              # use cases (escrita e consulta)
│   │   └── adapter/                  # ginhandler, entrepo, openfinance/mockprovider, persistencebench
│   └── platform/                     # infra cross-cutting (não é BC)
│       ├── config/ db/ outbox/       # env, pool e unit of work, outbox sobre o River
│       └── httperror/ httpmiddleware/# tradução de erro, CORS
├── migrations/<bounded-context>/     # SQL versionado gerado pelo Atlas
├── migrations/river/                 # SQL do River extraído com `make migrate-river-get`
└── docs/                             # diagramas, specs e pesquisa
```

## Status e roadmap

| Fase | Escopo | Status |
|---|---|---|
| **PR1** | Financial Tracking v1 + Identity com SSO Google: aggregates, use cases de escrita e consulta, API Gin, Postgres/Ent, dispatcher in-process, provider mock | 🚧 em andamento |
| PR2 | Outbox transacional + worker de entrega (River sobre o Postgres), importação atômica | 🚧 em andamento |
| PR3 | ~~Identity completo~~ — antecipado no PR1 via SSO Google | — |
| PR4 | Integração Open Finance real via Pluggy (webhooks, consent, reconciliação, idempotência do import) | backlog |
| PR5 | Goals & Budgets BC consumindo eventos de Financial Tracking | backlog |

A pesquisa técnica da API Pluggy que fundamenta o PR4 está em [`docs/research/pluggy-api.md`](docs/research/pluggy-api.md).

## Testes

```sh
make test              # unitários e de borda, com e sem a build tag devauth
make test-integration  # repositórios contra Postgres real (build tag `integration`)
make bench-persistence # latência e alocação: Ent vs SQL cru vs pgxpool
make check             # fmt + vet + test, o que rodar antes de commitar
```

Os testes de integração ficam atrás de build tag para o `go test ./...` do dia a dia não exigir Docker. O relatório do benchmark de persistência está em [`docs/benchmarks/`](docs/benchmarks/).

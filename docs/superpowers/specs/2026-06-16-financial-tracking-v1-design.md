# Financial Tracking v1 — Design Spec

**Data:** 2026-06-16
**Status:** Draft
**Linguagem-alvo:** Go 1.22+
**Projeto:** Gestor de gastos com Open Finance — primeira fatia (Financial Tracking BC)

> Este documento é o spec formal aprovado para implementação. Atualizações pontuais permitidas durante implementação só com nota no histórico. Mudanças de escopo exigem novo spec.

---

## Sumário

1. [Escopo e Backlog](#1-escopo-e-backlog)
2. [Stack e Convenções](#2-stack-e-convenções)
3. [Layout de Pacotes](#3-layout-de-pacotes)
4. [Modelagem de Aggregates](#4-modelagem-de-aggregates)
5. [Fluxo de Dados](#5-fluxo-de-dados)
6. [Eventos de Domínio](#6-eventos-de-domínio)
7. [Erros e Validação](#7-erros-e-validação)
8. [Estratégia de Testes](#8-estratégia-de-testes)
9. [Histórico](#9-histórico)

---

## 1. Escopo e Backlog

### Dentro do v1

- **Financial Tracking BC** (núcleo do produto)
  - Aggregate **Account** (root + VO `Balance` + VO `Source`)
  - Aggregate **Transaction** (root + VO `Money` + VO `ExternalRef` + VO `CategoryAssignment`)
  - Aggregate **Category** (root + entity `CategoryRule`)
- **Identity BC (mini)**: tabela `users` (id, email). Sem signup/login. Middleware Gin extrai `X-User-Id` do header → `context.Context`.
- **Origem de dados**: porta `OpenFinanceProvider` com 2 adapters
  - Entrada manual via API REST
  - `MockImporterAdapter` (gera dados sintéticos pra exercitar o fluxo de importação)
- **4 Domain Events**: `TransactionImported`, `TransactionCategorized`, `AccountBalanceUpdated`, `TransactionReconciled`. Dispatcher **in-process síncrono** (interface estável, troca para outbox em PR2).
- **Persistência**: PostgreSQL via `pgx` (sem ORM).
- **API HTTP**: Gin, handlers finos delegando a use cases.

### Fora do v1 — backlog explícito

| Item | Quando | Por que não entra agora |
|---|---|---|
| **Outbox pattern + worker assíncrono** | PR2 | Goroutines + transações + retry simultâneo na primeira semana de Go é overload. V1 usa dispatcher in-process com a MESMA interface — PR2 troca a implementação sem mexer no domínio. |
| **Identity BC completo** (signup, JWT, refresh) | PR3 | Crypto + claims + middleware de auth real merece spec próprio. |
| **Open Finance real** (Pluggy/Belvo adapter) | PR4 | Webhooks, consent management, sync jobs — outro spec. Porta `OpenFinanceProvider` já pronta. |
| **Goals & Budgets BC** | PR5 | Consome eventos de Financial Tracking. Depende do outbox em pé. |
| **Reconciliação** (`TransactionReconciled` ativo) | PR4 | Evento já modelado no v1; só dispara em fluxo Open Finance real. |
| **Category rules engine** (auto-categorização) | PR2 ou PR5 | Entity `CategoryRule` já existe; aplicador automático fica pra depois. |

### Garantias de design que protegem o backlog

1. **Dispatcher é interface no domain** — trocar in-process por outbox não toca em domain nem use case, só no wire em `main.go`.
2. **`OpenFinanceProvider` é interface no domain** — adicionar `PluggyAdapter` no PR4 é só registrar no `main.go`.
3. **`UserID` é VO no domain shared** — quando Identity completo entrar, middleware muda mas domínio não.
4. **Eventos `TransactionReconciled` e `TransactionCategorized` já modelados** mesmo sem consumidores externos — Goals & Budgets só adiciona handlers no PR5.

---

## 2. Stack e Convenções

### Bibliotecas

| Camada | Lib | Por que |
|---|---|---|
| HTTP | `github.com/gin-gonic/gin` | Popular no mercado BR. Boa documentação. |
| Persistência | `github.com/jackc/pgx/v5` | Driver Postgres mais usado. Sem ORM. |
| UUIDs | `github.com/google/uuid` | Padrão de facto. |
| Migrations | `github.com/golang-migrate/migrate/v4` | CLI + lib. SQL files versionados. |
| Testes | `testing` (stdlib) + `github.com/stretchr/testify` | Padrão em ~80% dos projetos Go. |
| Config | `github.com/kelseyhightower/envconfig` ou stdlib `os.Getenv` | Começa com stdlib; troca quando ficar repetitivo. |
| Logging | `log/slog` (stdlib, 1.21+) | Structured logging nativo. Sem zap/zerolog no v1. |

### Convenções rígidas

- **Sem framework escondendo HTTP**: handlers Gin usam `*gin.Context` mas tudo abaixo recebe `context.Context` puro.
- **Sem ORM**: queries SQL na mão.
- **Dinheiro como `int64` em centavos**: floats são proibidos em domínio financeiro.
- **`internal/`**: tudo privado ao módulo. Go enforça via compilador.
- **Domain importa de NADA fora dele**: nem application, nem adapter, nem platform.
- **Adapters implementam interfaces de domain estruturalmente**: sem `implements` declarado.

---

## 3. Layout de Pacotes

```
financial-manager/
├── cmd/api/main.go                              # composition root (DI manual)
├── internal/
│   ├── identity/                                # Bounded Context: Identity (mini v1)
│   │   ├── domain/
│   │   │   ├── user.go                          # aggregate User
│   │   │   └── repository.go                    # UserRepository interface
│   │   └── adapter/
│   │       ├── pgxrepo/user_repository.go       # implementa UserRepository
│   │       └── ginmiddleware/user_context.go    # extrai X-User-Id
│   │
│   ├── financialtracking/                       # Bounded Context: núcleo
│   │   ├── domain/
│   │   │   ├── account/                         # aggregate Account
│   │   │   │   ├── account.go
│   │   │   │   ├── balance.go                   # VO
│   │   │   │   ├── source.go                    # VO
│   │   │   │   ├── events.go                    # AccountBalanceUpdated
│   │   │   │   ├── errors.go                    # ErrCurrencyMismatch, ErrNotFound...
│   │   │   │   └── repository.go                # AccountRepository interface
│   │   │   ├── transaction/                     # aggregate Transaction
│   │   │   │   ├── transaction.go
│   │   │   │   ├── externalref.go               # VO
│   │   │   │   ├── categoryassignment.go        # VO
│   │   │   │   ├── events.go                    # 3 eventos
│   │   │   │   ├── errors.go
│   │   │   │   └── repository.go                # TransactionRepository interface
│   │   │   ├── category/                        # aggregate Category
│   │   │   │   ├── category.go
│   │   │   │   ├── rule.go                      # entity CategoryRule
│   │   │   │   ├── errors.go
│   │   │   │   └── repository.go                # CategoryRepository interface
│   │   │   ├── shared/                          # VOs e tipos compartilhados entre aggregates do BC
│   │   │   │   ├── userid.go                    # VO UserID
│   │   │   │   ├── currency.go                  # VO Currency
│   │   │   │   ├── money.go                     # VO Money (centavos)
│   │   │   │   ├── errors.go                    # ErrForbidden, ErrInvalidCurrency
│   │   │   │   └── events/
│   │   │   │       ├── event.go                 # Event interface
│   │   │   │       └── dispatcher.go            # Dispatcher interface
│   │   │   └── openfinance/                     # porta de integração externa
│   │   │       └── provider.go                  # OpenFinanceProvider interface + DTOs
│   │   │
│   │   ├── application/                         # use cases (orquestração) — SEM ports.go
│   │   │   ├── createaccount/usecase.go
│   │   │   ├── recordtransaction/usecase.go
│   │   │   ├── categorizetransaction/usecase.go
│   │   │   ├── importfromprovider/usecase.go
│   │   │   └── createcategory/usecase.go
│   │   │
│   │   └── adapter/                             # adapters de infraestrutura
│   │       ├── ginhandler/                      # in: HTTP
│   │       │   ├── account_handler.go
│   │       │   ├── transaction_handler.go
│   │       │   ├── category_handler.go
│   │       │   └── routes.go
│   │       ├── pgxrepo/                         # out: persistência
│   │       │   ├── account_repository.go
│   │       │   ├── transaction_repository.go
│   │       │   └── category_repository.go
│   │       └── openfinance/
│   │           └── mockprovider/mock_provider.go
│   │
│   └── platform/                                # infra cross-cutting (NÃO é BC)
│       ├── events/inprocess.go                  # impl do Dispatcher (interface em domain)
│       ├── db/pgx.go                            # pool + helpers tx
│       ├── httperror/error.go                   # mapping erro→HTTP
│       └── config/config.go                     # env vars
│
├── migrations/                                  # SQL versionado (golang-migrate)
│   ├── 0001_init_identity.up.sql
│   ├── 0001_init_identity.down.sql
│   ├── 0002_init_financial_tracking.up.sql
│   └── 0002_init_financial_tracking.down.sql
├── pkg/                                         # vazio no v1
├── go.mod
├── Makefile
└── docker-compose.yml                           # postgres local
```

### Regras de dependência (validadas pelo compilador via `internal/` e estrutura de imports)

```
adapter/  ────→  application/  ────→  domain/
   ↓                  │                  ↑
   └──────────────────┴──────────────────┘
                      ↑
               platform/  (depende de domain quando implementa interface dele)
```

- **domain/** importa de NADA fora de si mesmo (exceto `time`, `errors`, `uuid` da stdlib/lib).
- **application/** importa de domain/. Use case declara dependências via interfaces de domain.
- **adapter/** importa de domain/ (pra implementar interfaces) e às vezes de application/ (pra invocar use case — caso ginhandler).
- **platform/** implementa interfaces declaradas em domain/. Não pode ser importado por domain.
- **cmd/api/main.go** é o ÚNICO arquivo que importa concretos de tudo — composition root.

### Conceitos Go que o layout ensina

| Conceito DDD/SOLID | Idioma Go que materializa |
|---|---|
| `internal/` privado ao módulo | Convenção da linguagem, enforçada pelo compilador |
| Aggregate encapsulado | Campos lowercase + package boundary |
| Repository interface | Interface no package do aggregate |
| Implementação de interface | Estrutural (duck typing) — sem `implements` |
| Dependency Inversion | Interfaces no domain, implementações no adapter, wire em main |
| Anti-Corruption Layer | DTOs `ProviderTransaction` em `domain/openfinance/`, traduzidos no use case `importfromprovider` |

---

## 4. Modelagem de Aggregates

### Regras invariantes (válidas pros 3 aggregates)

**1. Aggregate Root = struct com campos minúsculos.**
Acesso externo só por métodos. Mutações só por métodos do aggregate.
```go
type Account struct {
    id      AccountID
    userID  shared.UserID
    name    string
    kind    Kind
    balance Balance
    source  Source
    events  []events.Event
}
```

**2. Dois construtores: `New(...)` e `Reconstitute(...)`.**
- `New` valida invariantes — usado pelos use cases ao criar do zero. Retorna `(*Account, error)`.
- `Reconstitute` confia nos dados — usado pelos repositories ao hidratar do banco. Sem retorno de erro.

**3. Cross-aggregate reference só por ID.**
`Transaction.accountID account.AccountID` — nunca `*account.Account`. Garante que aggregates carreguem/persistam independentemente.

**4. VOs imutáveis, retornados por valor.**
Money, Balance, ExternalRef, UserID, Currency — todos struct com campos minúsculos + métodos read-only + zero setters. Mudar = criar novo VO.

**5. Eventos acumulados no aggregate, despachados pelo use case.**
```go
func (a *Account) UpdateBalance(newBal Balance) error {
    // ... validações
    a.balance = newBal
    a.events = append(a.events, NewAccountBalanceUpdated(...))
    return nil
}

func (a *Account) Events() []events.Event { return a.events }
func (a *Account) ClearEvents()           { a.events = nil }
```

**6. Erros do domínio são sentinels + erros estruturados.**
```go
// internal/financialtracking/domain/account/errors.go
package account

var (
    ErrCurrencyMismatch = errors.New("account: currency mismatch")
    ErrNotFound         = errors.New("account: not found")
    ErrInvalidName      = errors.New("account: invalid name")
)
```
`errors.Is/As` no httperror traduz para HTTP.

### Onde fica `Money`

`Money` é usado por **Account** (via `Balance`) e por **Transaction`. Decisão:
- **Money vive em `domain/financialtracking/shared/money.go`** (não em `domain/transaction/`).
- Justificativa: VO realmente compartilhado entre aggregates do MESMO BC. Cabe em `shared/`.

### Aggregate Account — superfície pública

```go
package account

func New(userID shared.UserID, name string, kind Kind, currency shared.Currency, source Source) (*Account, error)
func Reconstitute(id AccountID, userID shared.UserID, name string, kind Kind, balance Balance, source Source) *Account

func (a *Account) ID() AccountID
func (a *Account) UserID() shared.UserID
func (a *Account) Name() string
func (a *Account) Kind() Kind
func (a *Account) Balance() Balance
func (a *Account) Source() Source

func (a *Account) UpdateBalance(newBal Balance) error  // emite AccountBalanceUpdated
func (a *Account) Rename(newName string) error

func (a *Account) Events() []events.Event
func (a *Account) ClearEvents()
```

### Aggregate Transaction — superfície pública

```go
package transaction

func NewManual(userID shared.UserID, accountID account.AccountID, money shared.Money, occurredAt time.Time, description string) (*Transaction, error)
func NewFromProvider(userID shared.UserID, accountID account.AccountID, money shared.Money, occurredAt time.Time, description string, ref ExternalRef) (*Transaction, error)  // emite TransactionImported
func Reconstitute(...) *Transaction

func (t *Transaction) ID() TransactionID
func (t *Transaction) AccountID() account.AccountID
func (t *Transaction) UserID() shared.UserID
func (t *Transaction) Money() shared.Money
func (t *Transaction) OccurredAt() time.Time
func (t *Transaction) Description() string
func (t *Transaction) Category() *CategoryAssignment  // nil se não categorizada
func (t *Transaction) ExternalRef() *ExternalRef     // nil se manual

func (t *Transaction) Categorize(categoryID category.CategoryID, by AssignedBy) error  // emite TransactionCategorized
func (t *Transaction) MarkReconciled() error                                            // emite TransactionReconciled

func (t *Transaction) Events() []events.Event
func (t *Transaction) ClearEvents()
```

### Aggregate Category — superfície pública

```go
package category

func New(userID shared.UserID, name string, parentID *CategoryID) (*Category, error)
func Reconstitute(...) *Category

func (c *Category) ID() CategoryID
func (c *Category) UserID() shared.UserID
func (c *Category) Name() string
func (c *Category) ParentID() *CategoryID
func (c *Category) Rules() []CategoryRule  // retorna cópia

func (c *Category) Rename(newName string) error
func (c *Category) MoveTo(newParentID *CategoryID) error
func (c *Category) AddRule(rule CategoryRule) error
func (c *Category) RemoveRule(ruleID RuleID) error
```

Category **não emite eventos no v1** (nenhum consumidor downstream).

---

## 5. Fluxo de Dados

### Trajetória típica de um POST

```
HTTP request
   │
   ▼
gin middleware UserContext  ──► injeta UserID no context.Context
   │
   ▼
gin handler  (adapter/ginhandler/transaction_handler.go)
   │  - bind body → request DTO local
   │  - lê UserID do context
   │  - mapeia DTO + UserID → use case Input
   │
   ▼
use case  (application/recordtransaction/usecase.go)
   │  - depende de TransactionRepository, AccountRepository, Dispatcher (interfaces de domain)
   │  - lê aggregate Account pra validar ownership
   │  - constrói Money (shared VO)
   │  - chama transaction.NewManual(...) — domínio valida
   │  - chama transactionRepo.Save(ctx, tx)
   │  - chama dispatcher.Dispatch(ctx, tx.Events()...)
   │  - tx.ClearEvents()
   │
   ▼
domain  (domain/transaction/transaction.go)
   │  - aplica invariantes
   │  - acumula eventos no aggregate
   │
   ▼
pgxrepo  (adapter/pgxrepo/transaction_repository.go)
   │  - implementa TransactionRepository
   │  - traduz aggregate → linhas SQL
   │  - converte erros pgx → erros de domain
   │
   ▼
PostgreSQL
```

### Composition Root (`cmd/api/main.go`)

```go
func main() {
    cfg, _ := config.Load()
    pool, _ := db.NewPool(ctx, cfg.DatabaseURL)
    defer pool.Close()

    // adapters concretos
    accountRepo := pgxrepo.NewAccountRepository(pool)
    transactionRepo := pgxrepo.NewTransactionRepository(pool)
    categoryRepo := pgxrepo.NewCategoryRepository(pool)
    provider := mockprovider.New()
    dispatcher := platformevents.NewInProcessDispatcher()

    // (opcional v1) registro de event handlers in-process
    // dispatcher.Register(transaction.EventTypeImported, someHandler)

    // use cases
    createAccount := createaccount.NewUseCase(accountRepo, dispatcher)
    recordTx := recordtransaction.NewUseCase(transactionRepo, accountRepo, dispatcher)
    categorizeTx := categorizetransaction.NewUseCase(transactionRepo, categoryRepo, dispatcher)
    importTx := importfromprovider.NewUseCase(transactionRepo, accountRepo, provider, dispatcher)
    createCategory := createcategory.NewUseCase(categoryRepo)

    // HTTP
    accountH := ginhandler.NewAccountHandler(createAccount)
    transactionH := ginhandler.NewTransactionHandler(recordTx, categorizeTx, importTx)
    categoryH := ginhandler.NewCategoryHandler(createCategory)

    r := gin.Default()
    r.Use(ginmiddleware.UserContext())
    ginhandler.RegisterRoutes(r, accountH, transactionH, categoryH)

    r.Run(cfg.HTTPAddr)
}
```

Esse arquivo é o ÚNICO lugar com instâncias concretas. Trocar Postgres por Mongo = trocar o tipo do `accountRepo` aqui. Trocar in-process por outbox = trocar o tipo do `dispatcher` aqui.

### Endpoints v1

| Método | Path | Use case |
|---|---|---|
| POST | `/v1/accounts` | createaccount |
| POST | `/v1/transactions` | recordtransaction |
| PUT | `/v1/transactions/:id/category` | categorizetransaction |
| POST | `/v1/transactions/import` | importfromprovider |
| POST | `/v1/categories` | createcategory |

Endpoints de leitura (GET) ficam opcionais no v1. Recomendado incluir `GET /v1/accounts/:id` e `GET /v1/transactions/:id` mínimos para verificação manual.

---

## 6. Eventos de Domínio

### Interface base

```go
// internal/financialtracking/domain/shared/events/event.go
package events

import "time"

type Event interface {
    EventName() string
    OccurredAt() time.Time
}
```

### Dispatcher (interface no domain, implementação em platform)

```go
// internal/financialtracking/domain/shared/events/dispatcher.go
package events

import "context"

type Handler func(ctx context.Context, event Event) error

type Dispatcher interface {
    Register(eventName string, handler Handler)
    Dispatch(ctx context.Context, events ...Event) error
}
```

```go
// internal/platform/events/inprocess.go
package events

import (
    "context"

    domainevents "github.com/<org>/financial-manager/internal/financialtracking/domain/shared/events"
)

type InProcessDispatcher struct {
    handlers map[string][]domainevents.Handler
}

func NewInProcessDispatcher() *InProcessDispatcher
func (d *InProcessDispatcher) Register(name string, h domainevents.Handler)
func (d *InProcessDispatcher) Dispatch(ctx context.Context, events ...domainevents.Event) error
```

### Eventos do v1

| Evento | Emissor | Quando | Consumidor v1 | Consumidor futuro |
|---|---|---|---|---|
| `TransactionImported` | Transaction | `NewFromProvider` | nenhum (log) | Goals & Budgets (PR5) |
| `TransactionCategorized` | Transaction | `Categorize` | nenhum (log) | Budgets (PR5) |
| `AccountBalanceUpdated` | Account | `UpdateBalance` | nenhum (log) | Goals (PR5) |
| `TransactionReconciled` | Transaction | `MarkReconciled` | nenhum (log) | Reconciliação (PR4) |

### Regras

- **Somente Aggregate Root emite eventos.** Repository não. Use case não.
- **Use case dispatcha APÓS persistir**: ordem é `Save → Dispatch → ClearEvents`.
- **Eventos são imutáveis VOs** com nomes no passado.
- **Construtor explícito por evento** (Event Mapper). Nada de reflection.
- **No v1, dispatcher in-process síncrono**: se handler falha, dispatch retorna erro e use case retorna erro. Sem retry.
- **PR2 troca a impl pra Outbox**: aggregate continua emitindo igual, interface não muda.

---

## 7. Erros e Validação

### Camadas de erro

| Origem | Tipo | Tradução HTTP |
|---|---|---|
| Validação de input (struct binding) | erro do framework Gin | 400 |
| Invariante de domínio violada | sentinel error (ex: `account.ErrCurrencyMismatch`) | 422 |
| Aggregate não encontrado | sentinel (ex: `account.ErrNotFound`) | 404 |
| Ownership violada (user X tentando acessar de Y) | sentinel (ex: `ErrForbidden` em domain/shared) | 403 |
| Erro de banco genérico | erro pgx wrapped | 500 (não exposto) |

### Sentinels por aggregate

```go
// domain/account/errors.go
var (
    ErrCurrencyMismatch = errors.New("account: currency mismatch")
    ErrNotFound         = errors.New("account: not found")
    ErrInvalidName      = errors.New("account: invalid name")
)

// domain/transaction/errors.go
var (
    ErrZeroMoney    = errors.New("transaction: amount must be non-zero")
    ErrNotFound     = errors.New("transaction: not found")
    ErrInvalidRef   = errors.New("transaction: invalid external ref")
)

// domain/category/errors.go
var (
    ErrNotFound        = errors.New("category: not found")
    ErrDuplicateRule   = errors.New("category: duplicate rule")
    ErrInvalidName     = errors.New("category: invalid name")
)

// domain/shared/errors.go
var (
    ErrForbidden       = errors.New("forbidden")
    ErrInvalidCurrency = errors.New("invalid currency")
)
```

### Tradução para HTTP (`internal/platform/httperror/error.go`)

```go
func Write(c *gin.Context, err error) {
    switch {
    case errors.Is(err, account.ErrNotFound),
         errors.Is(err, transaction.ErrNotFound),
         errors.Is(err, category.ErrNotFound):
        c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})

    case errors.Is(err, shared.ErrForbidden):
        c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})

    case errors.Is(err, account.ErrCurrencyMismatch),
         errors.Is(err, transaction.ErrZeroMoney),
         errors.Is(err, shared.ErrInvalidCurrency):
        c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})

    default:
        slog.ErrorContext(c.Request.Context(), "internal error", "err", err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
    }
}
```

Mensagens internas NUNCA vazam em 500 (só vai pro log).

### Erros nas fronteiras (repositórios)

Repositórios traduzem erros pgx → erros de domain:
```go
if errors.Is(err, pgx.ErrNoRows) {
    return nil, account.ErrNotFound
}
```

### Validação de input HTTP

- Tags Gin `binding:"required"` cobrem casos básicos (campo presente).
- Validações de formato (UUID, currency) acontecem nos CONSTRUTORES de VOs (`shared.NewUserID(s)`, `shared.NewCurrency(s)`).
- Handler chama o construtor, recebe erro, joga em `httperror.Write`.

---

## 8. Estratégia de Testes

### Pirâmide

| Tipo | Onde | O que testa | Quantidade |
|---|---|---|---|
| Unit do domain | `domain/<aggregate>/*_test.go` | Invariantes, transições de estado, emissão de eventos | Maioria |
| Unit do use case | `application/<usecase>/*_test.go` (opcional v1) | Orquestração, com mocks das interfaces de domain | Algumas |
| Integração de repository | `adapter/pgxrepo/*_test.go` | Save + Find round-trip contra Postgres real | Por aggregate |
| Integração HTTP | `adapter/ginhandler/*_test.go` (opcional v1) | Endpoints chamando use cases reais | Smoke |

### Convenções

- **Package `<x>_test`** (externa) testa API pública. Para testar funções não-exportadas, criar arquivo `internal_test.go` com `package <x>`.
- **Table-driven tests** com `t.Run` para subtests.
- **`testify/require`** no setup (para se falhar, o teste para — não polui logs com asserts secundários).
- **`testify/assert`** em verificações múltiplas.
- **`t.Parallel()`** em testes do domain (puros).
- **Test helpers** em `test_helpers.go` no package `<x>_test`.

### Esquema de teste do domain (exemplo)

```go
package account_test

func TestNewAccount(t *testing.T) {
    t.Parallel()
    cases := []struct {
        name    string
        userID  shared.UserID
        // ...
        wantErr error
    }{
        {"happy path", validUserID, ..., nil},
        {"empty name", validUserID, ..., account.ErrInvalidName},
    }
    for _, tc := range cases {
        tc := tc
        t.Run(tc.name, func(t *testing.T) {
            t.Parallel()
            got, err := account.New(tc.userID, ...)
            require.ErrorIs(t, err, tc.wantErr)
            if tc.wantErr == nil {
                require.NotNil(t, got)
            }
        })
    }
}

func TestAccount_UpdateBalance_EmitsEvent(t *testing.T) {
    t.Parallel()
    acc := newValidAccount(t)
    require.NoError(t, acc.UpdateBalance(newValidBalance(t)))

    events := acc.Events()
    require.Len(t, events, 1)
    require.Equal(t, account.EventTypeBalanceUpdated, events[0].EventName())
}
```

### Testes de integração de repository

Estratégia: testcontainers-go com Postgres real, ou banco compartilhado de teste. Para v1, sugestão pragmática:
- `make test-integration` sobe `docker compose up postgres` se necessário.
- Test usa `pgx` direto, faz `TRUNCATE` antes de cada teste.
- Não corre por padrão em `go test ./...` — usar build tag `//go:build integration`.

### Não fazer no v1

- E2E completo (subir API + cliente HTTP).
- Mock de pgx (testes de repo SEMPRE contra Postgres real).
- Testes geradores de carga.

---

## 9. Histórico

| Data | Mudança |
|---|---|
| 2026-06-16 | Versão inicial submetida para revisão. Decisão arquitetural firmada: ports vivem em `domain/` (Evans-style), `application/` só tem use cases. |

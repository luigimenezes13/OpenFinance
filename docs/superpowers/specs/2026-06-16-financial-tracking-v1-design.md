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
| Persistência | `entgo.io/ent` (+ `github.com/jackc/pgx/v5/stdlib` como driver) | Schema como código → client tipado gerado. Papel do Prisma no ecossistema Go. Trocado em 2026-08-25; antes era pgx cru. |
| UUIDs | `github.com/google/uuid` | Padrão de facto. |
| Migrations | `ariga.io/atlas` via `ent/migrate` (feature `sql/versioned-migration`) | SQL versionado GERADO do diff do schema — o `prisma migrate dev`. Forward-only (sem `.down.sql`). |
| Testes | `testing` (stdlib) + `github.com/stretchr/testify` | Padrão em ~80% dos projetos Go. |
| Config | `github.com/kelseyhightower/envconfig` ou stdlib `os.Getenv` | Começa com stdlib; troca quando ficar repetitivo. |
| Logging | `log/slog` (stdlib, 1.21+) | Structured logging nativo. Sem zap/zerolog no v1. |

### Convenções rígidas

- **Sem framework escondendo HTTP**: handlers Gin usam `*gin.Context` mas tudo abaixo recebe `context.Context` puro.
- **ORM com codegen, não SQL na mão** (revisto em 2026-08-25): o schema de persistência vive em `adapter/entrepo/ent/schema` e o client tipado é gerado. O SQL das migrations também é gerado — ninguém escreve `CREATE TABLE`.
- **As entities do Ent NÃO são o domínio**: são modelo de persistência; a ponte é o Snapshot do aggregate (ent → Snapshot → FromSnapshot). Nenhum import de `ent` fora de `adapter/entrepo` e do composition root.
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

**Autenticação (2026-08-26):** todas as rotas `/v1` exigem `Authorization: Bearer <google_id_token>`. `GET /healthz` fica fora do grupo autenticado (orquestrador não manda credencial). O middleware do BC Identity verifica o token, resolve o usuário local — provisionando no primeiro acesso — e injeta o uuid no `context.Context`.

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
| 2026-07-07 | `Reconstitute` REMOVIDO da superfície pública dos aggregates (correção de fronteira): rehidratação é responsabilidade dos mappers toDomain/fromDomain do repository, não operação de domínio. Padrão decidido para o PR dos repos: **Snapshot/Memento** — par `Snapshot()`/`FromSnapshot()` no domínio como ponto de montagem, mapper converte row↔Snapshot (em Go, campos unexported impedem o mapper de montar o aggregate sem um ponto exportado). |
| 2026-07-04 | Fronteira de tradução firmada (padrão toDomain/fromDomain): o domínio NÃO parseia representações externas. Identidades são `shared.TypedID[T]` (generics, phantom type) construídas via `TypedIDFromUUID(uuid.UUID)` — parsing string→UUID e tradução de erro de formato vivem nos mappers das bordas (middleware, handlers, repositories). O trecho de §7 sobre "validações de formato nos construtores de VOs" segue valendo APENAS pra VOs de vocabulário (Currency, Kind, Source), cujo texto é a própria linguagem do domínio. |
| 2026-07-04 | Validação do modelo Account contra a doc Pluggy (`docs/research/pluggy-api.md`). Duas mudanças: (1) VO `Source` enriquecido — origem + referência do provider (provider, providerAccountID), com a invariante "openfinance tem ref, manual não tem" garantida por dois construtores; necessário pra resolver conta do provider → Account local no import. (2) Convenção de saldo-patrimônio: `Balance` significa sempre "quanto a conta soma ao patrimônio" — dívida de cartão é NEGATIVA, ACL normaliza a fatura positiva da Pluggy na entrada. |
| 2026-08-25 | Application layer fechada (5 use cases + porta `openfinance`). Layout ajustado: UM package `application` com um arquivo por use case (`create_account.go`, ...), em vez de um sub-package por use case como em §3 — idioma Go, Input/Output/mapper colados no use case que os define. Quatro decisões: (1) **Transação herda a moeda da conta** — `RecordTransactionInput` não tem campo de moeda; deixar o cliente mandá-la criaria a invariante "moeda da transação = moeda da conta", que não cabe em nenhum dos dois aggregates e cairia no use case (que não pode ter regra). (2) **Dispatcher injetado só onde há evento** — `CreateAccount`, `CreateCategory` e `RecordTransaction` NÃO recebem dispatcher (os construtores desses aggregates não emitem nada); §5 previa um em `createaccount`/`recordtransaction` e isso seria dependência morta. (3) **`RecordTransaction` não mexe no saldo** — saldo de conta conectada é fato do provider; derivar saldo de lançamento manual é comportamento próprio (backlog), não efeito colateral escondido. (4) **`ImportFromProvider` não é idempotente no v1** — deduplicar exige consultar por `ExternalRef` (método ausente na porta `transaction.Repository`) e a doc Pluggy §5.5 mostra que o id do provider não serve de chave; reimportar duplica, e falha no meio aborta deixando o já-salvo salvo. Ambos entram no PR4 junto com a transação de banco. Ownership (`ErrForbidden`) é checada no use case, não no aggregate: autorização é regra de aplicação. |
| 2026-08-25 | Persistência fechada: par Snapshot/FromSnapshot nos 3 aggregates, migration inicial, 3 repositórios pgx, pool em `platform/db` e suíte de integração contra Postgres real. Decisões: (1) **Rehidratar revalida tudo pelos construtores dos VOs**, mas NÃO reaplica invariante de momento-da-escrita — `FromSnapshot` de Transaction aceita data futura (só recusa instante zero), senão um lançamento gravado com relógio adiantado ficaria ilegível pra sempre. (2) **Regras de categoria voltam pelo `AddRule` do root**, não por montagem direta: um segundo caminho de montagem aceitaria o que o AddRule recusa (keyword duplicada). `normalizeKeyword` foi extraído do `NewCategoryRule` pra escrita e leitura usarem o mesmo critério. (3) **Sem FK entre aggregates** (`transactions.account_id`, `transactions.category_id`, `categories.parent_id` são UUID indexado): são fronteiras de consistência separadas, e FK acopla ciclo de vida. **Com FK + CASCADE dentro do aggregate** (`category_rules` → `categories`). `user_id` sem FK por ser referência cruzada de BC. (4) **CHECK só pra invariante estrutural** (par de origem coerente, atribuição de categoria completa, conciliada exige ref, valor não-zero); vocabulário (kind/currency/assigned_by) fora do schema, pra não exigir migration por moeda nova nem manter duas listas divergentes. (5) **`Save` é upsert** (`ON CONFLICT (id) DO UPDATE`) — a porta tem um método só, quem decide insert/update é o SQL. Regras de categoria usam "apaga e regrava" numa transação: o aggregate é a fonte da verdade do conjunto. (6) **`pgx.ErrNoRows` → `ErrNotFound` do aggregate**; qualquer outra falha sai embrulhada com `%w`. (7) **Índice único parcial** em (source_provider, source_provider_account_id) `WHERE source_provider <> ''` — sem o WHERE, a segunda conta manual do sistema colidiria. Nenhum unique em external_ref: idempotência segue no PR4. (8) Testes de integração atrás da build tag `integration`, sequenciais (banco compartilhado), com as migrations aplicadas via pgx no TestMain; comparação de tempo por tolerância porque TIMESTAMPTZ trunca em microssegundo. |
| 2026-08-25 | **Persistência trocada de pgx cru para Ent + Atlas** (pedido do autor: preferência por stack no espírito de Prisma/Kysely em vez de SQL e DDL escritos à mão). O que mudou: `adapter/pgxrepo` → `adapter/entrepo` (client tipado, upsert via `OnConflict`, eager loading via `WithRules`), `migrations/` passou a ser SQL GERADO pelo `ent/migrate` + Atlas em modo replay, e `platform/db` passou a devolver `*sql.DB` (pgx como driver do `database/sql`) porque é o que o Ent consome — `pgxpool` e o helper `InTx` saíram (o Ent tem transação própria). **O que NÃO mudou: nenhuma linha de `domain/` ou `application/`** — a prova prática de que as portas estão no lugar certo; os 87 testes de application seguiram passando sem edição. Decisões preservadas na tradução: sem FK entre aggregates (campos UUID puros, edge só em `category → rules` com CASCADE), CHECK apenas de invariante estrutural (via `entsql.Checks`), índice único parcial da referência do provider (via `entsql.IndexWhere`), nenhum unique em `external_ref` (idempotência segue no PR4). Custo aceito: ~17k linhas geradas commitadas em `adapter/entrepo/ent/`, e o gerador de migration precisa de um banco dev descartável (`ATLAS_DEV_DATABASE_URL`) porque o Atlas reaplica o histórico pra calcular o diff. Migrations do Atlas são forward-only: o reset de ambiente de teste é `DROP SCHEMA public CASCADE`. |
| 2026-08-26 | **BC Identity implementado com autenticação delegada ao Google (OIDC)**, substituindo o `X-User-Id` confiável do v1. Fluxo escolhido: o cliente obtém o ID token no Google e o envia em cada request; o backend valida assinatura/issuer/audience/expiração (JWKS em cache) e resolve o usuário local. Descartado o authorization-code flow com sessão própria: exigiria state store, PKCE, cookies e gestão de chave de assinatura, sem ganho no v1. Decisões: (1) **O `sub` do provedor é referência externa, não identidade do sistema** — VO `ExternalIdentity`, terceiro uso do padrão de `account.Source`/`transaction.ExternalRef`. Sem isso, cada tabela de cada BC ficaria chaveada por string de fornecedor e "entrar com Apple" viraria migration em toda tabela. (2) **Cada BC tem o seu UserID** (`identity.UserID` e `shared.UserID` são tipos distintos que carregam o mesmo uuid); a tradução entre contextos é em uuid puro, na borda. (3) **Provisionamento just-in-time**: token válido de quem o sistema não conhece só pode significar primeiro acesso — endpoint de cadastro separado criaria o estado insustentável "token válido de pessoa inexistente". (4) **E-mail não atestado pelo provedor é recusado** (`email_verified`), senão alguém se registraria com o endereço de outra pessoa. (5) **Audience obrigatória**, com falha no boot: sem validá-la, o serviço aceitaria token legítimo do Google emitido para outro aplicativo — falha clássica de OIDC. (6) **Identidade é (provedor, subject), não e-mail**: unique no par, e-mail apenas indexado, para um segundo provedor não recusar cadastro legítimo. (7) O evento `Registered` **não carrega o subject**: evento é contrato público entre contextos, e vazar a referência externa desfaria o encapsulamento do VO. (8) **Um schema/client/migration Ent por bounded context** (revisão de uma decisão anterior de schema único): o schema único forçaria o Identity a importar o client que vive em `financialtracking/adapter`, e com clients separados é o compilador que garante a fronteira. Custo conhecido, documentado no código: o sign-in roda em toda requisição e faz uma consulta ao banco; as saídas quando incomodar são cache (subject → uuid) ou token próprio, nenhuma das quais muda a fronteira. Ainda ausente: o aggregate User não tem fluxo de exclusão/desativação, e não há endpoint `GET /v1/me`. |

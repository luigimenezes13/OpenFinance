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

- **Ports vivem no domain** (estilo Evans): repositories, dispatcher e a porta `OpenFinanceProvider` são interfaces declaradas dentro do domínio; `application/` contém apenas use cases.
- **Dinheiro é `int64` em centavos** encapsulado no VO `Money`. Float é proibido em domínio financeiro.
- **Sem ORM**: persistência com `pgx` e SQL escrito à mão.
- **Aggregates encapsulados**: campos não-exportados, mutação só por métodos do root, eventos de domínio emitidos apenas pelo aggregate root.
- **Composition root único** (`cmd/api/main.go`): o único lugar do sistema que conhece tipos concretos. Trocar Postgres, provider ou dispatcher não toca em domínio nem use cases.

O design completo da primeira fatia está em [`docs/superpowers/specs/2026-06-16-financial-tracking-v1-design.md`](docs/superpowers/specs/2026-06-16-financial-tracking-v1-design.md).

## Stack

| Camada | Escolha |
|---|---|
| Linguagem | Go 1.26 |
| HTTP | Gin |
| Persistência | PostgreSQL + pgx (sem ORM) |
| Migrations | golang-migrate |
| Testes | stdlib `testing` + testify |
| Logging | `log/slog` (stdlib) |

## Estrutura do repositório

```
├── cmd/api/                      # composition root (DI manual)        [planejado]
├── internal/
│   ├── identity/                 # BC Identity                         [planejado]
│   ├── financialtracking/        # BC núcleo
│   │   ├── domain/               # aggregates, VOs, events, ports
│   │   │   └── shared/           # VOs compartilhados entre aggregates ← você está aqui
│   │   ├── application/          # use cases                           [planejado]
│   │   └── adapter/              # Gin handlers, pgx repos, providers  [planejado]
│   └── platform/                 # infra cross-cutting (não é BC)      [planejado]
├── migrations/                   # SQL versionado                      [planejado]
└── docs/                         # diagramas, specs e pesquisa
```

## Status e roadmap

O projeto está em desenvolvimento ativo, construído em fatias verticais:

| Fase | Escopo | Status |
|---|---|---|
| **PR1** | Financial Tracking v1: aggregates Account/Transaction/Category, use cases, API Gin, Postgres, dispatcher in-process, provider mock | 🚧 em andamento |
| PR2 | Outbox pattern + processamento assíncrono de eventos | backlog |
| PR3 | Identity completo (signup, JWT) | backlog |
| PR4 | Integração Open Finance real via Pluggy (webhooks, consent, reconciliação) | backlog |
| PR5 | Goals & Budgets BC consumindo eventos de Financial Tracking | backlog |

Implementado até aqui: VOs `Currency` e `Money` do shared kernel do Financial Tracking, com 100% de cobertura de testes.

A pesquisa técnica da API Pluggy que fundamenta o PR4 está em [`docs/research/pluggy-api.md`](docs/research/pluggy-api.md).

## Rodando os testes

```sh
go test ./... -cover
```

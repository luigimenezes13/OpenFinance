# Outbox Transacional + Worker (PR2) — Design Spec

**Data:** 2026-11-04
**Status:** Draft
**Linguagem-alvo:** Go 1.26+
**Projeto:** Gestor de gastos com Open Finance — troca do dispatcher in-process por outbox transacional

> Este documento é o spec formal para implementação. Atualizações pontuais permitidas durante implementação só com nota no histórico. Mudanças de escopo exigem novo spec.

---

## Sumário

1. [Motivação](#1-motivação)
2. [Decisões e Não-Objetivos](#2-decisões-e-não-objetivos)
3. [Pesquisa que embasou o desenho](#3-pesquisa-que-embasou-o-desenho)
4. [Layout de Pacotes](#4-layout-de-pacotes)
5. [Atomicidade: repositório dono da tx, Unit of Work como exceção](#5-atomicidade)
6. [Contrato de evento: envelope e mappers](#6-contrato-de-evento)
7. [Entrega: River como fila](#7-entrega-river-como-fila)
8. [Migrations](#8-migrations)
9. [Composition Root e ciclo de vida](#9-composition-root-e-ciclo-de-vida)
10. [Estratégia de Testes](#10-estratégia-de-testes)
11. [Histórico](#11-histórico)

---

## 1. Motivação

No v1 o use case faz `Save → Dispatch → ClearEvents`, com um `InProcessDispatcher` síncrono. Dois defeitos que o backlog do v1 já previa:

- **Evento perdido**: se o processo morre entre o `Save` e o `Dispatch`, o fato está gravado e ninguém fica sabendo. Com consumidores reais (Goals & Budgets no PR5, auxiliar de investimentos depois) isso é orçamento que não fecha.
- **Acoplamento de falha**: handler que falha aborta o use case DEPOIS do commit — o cliente recebe erro de uma operação que aconteceu.

O outbox resolve os dois: o evento é gravado **na mesma transação** do aggregate, e um worker o entrega depois, com retry.

## 2. Decisões e Não-Objetivos

### Decisões

| # | Decisão | Razão |
|---|---|---|
| D1 | **Repositório é dono da transação**: `Save` grava o aggregate E os eventos pendentes dele na mesma tx | Caminho padrão do Three Dots Labs e do `sklinkert/go-ddd`. O use case não tem como "esquecer a tx" porque não a vê. |
| D2 | **Unit of Work só onde há vários aggregates** (`ImportFromProvider`) | O Three Dots Labs trata o UoW como exceção ("shouldn't become your go-to tool"). Hoje é o único use case que grava N transações + a conta. |
| D3 | **A tx do UoW viaja no `context`**, e os clients Ent a encontram via um driver próprio (`AmbientDriver`) | Cada BC tem seu client Ent gerado; `ent.NewTxContext` é por package e não conecta os dois. Montar client sobre `*sql.Tx` dá panic em `UpdateOne` e `Tx()`. |
| D4 | **Porta `UnitOfWork` no kernel** (`internal/kernel`) | Regra do projeto: port é domínio. A pesquisa sugeria `application`; mantida a regra. |
| D5 | **Envelope + payload JSON versionado**, um mapper dedicado por evento | Payload é o contrato entre BCs: o consumidor de outro BC não importa tipo do produtor. |
| D6 | **River (`riverqueue/river`) como fila e worker**, via driver `riverdatabasesql` | Já traz lease com `SKIP LOCKED`, rescuer, backoff `attempt^4` com jitter, estado `discarded`, limpeza e shutdown gracioso. Decisão do autor contra a recomendação de escrever o próprio. |
| D7 | **Um job por (evento, assinante)**, único por args | Fan-out do River: cada handler tem seu retry e seu dead letter; o segundo handler falhando não reexecuta o primeiro. |
| D8 | **Worker roda como goroutine na API** | Um processo só. Escalar réplicas da API escala o worker; o River coordena via `SKIP LOCKED` e eleição de líder. |
| D9 | **Importação passa a ser atômica**, com a busca no provider FORA da tx | Rede dentro de tx segura lock. Falha no meio não deixa metade salva (o v1 deixava). Idempotência por `ExternalRef` continua no PR4. |

### Não-objetivos

| Item | Por que não entra agora |
|---|---|
| **Consumidores reais** | Goals & Budgets é o PR5. Aqui o único assinante é o de log. |
| **LISTEN/NOTIFY** | Poll-only é a base (funciona atrás de pgbouncer). `NewWithPgxListener` fica para quando latência incomodar. |
| **Reprocessamento de dead letter por API** | No PR2 é o `river` CLI / `JobRetry` documentado no README. |
| **Inbox no consumidor** | A dedup por `(event_id, subscriber)` é responsabilidade de cada consumidor real, no PR5. |
| **Ordem global** | River não garante ordem entre jobs. Nenhum consumidor atual precisa; registrado como limite. |

## 3. Pesquisa que embasou o desenho

Código lido (clones rasos), não só README:

- **Three Dots Labs** — `go-web-app-antipatterns/04-transactions` e o post *Database transactions in Go with layered architecture*. Padrão default: `UpdateFn` no repositório. Transaction Provider (UoW) só em casos de borda. No exemplo de outbox (`05-distributed-transactions/03-outbox`) quem grava o evento é o repositório, dentro da tx.
- **`sklinkert/go-ddd`** — `Save` abre tx, grava o aggregate e `PullEvents()` no outbox, commita.
- **`avito-tech/go-transaction-manager`** e **go-kratos examples** — tx no ctx com `DefaultTrOrDB(ctx, db)`; propagação `Required` (junta-se à tx aberta). Sem adapter para Ent.
- **Ent** (`dialect/sql/driver.go`, `sqlgraph/graph.go`, templates `base.tmpl`/`tx.tmpl`) — client sobre `*sql.Tx` quebra em `DB()`/`Tx()`/`UpdateOne`; a saída sugerida pelo mantenedor (ent/ent#3583) é decorar o `dialect.Driver`. Daí o `AmbientDriver`, que devolve `dialect.NopTx` quando já há tx no ctx.
- **River** (v0.51) — `JobGetAvailable` com `FOR UPDATE SKIP LOCKED` numa tx curta (não segura tx durante o handler), `JobRescuer`, retry `attempt^4` ±10%, `discarded` após `MaxAttempts` (25), `JobCleaner` (24h completed, 7d discarded), `riverdatabasesql` totalmente funcional em modo poll-only.
- **watermill-sql, pkritiotis/go-outbox, pgx-outbox, oagudo/outbox** — referências secundárias. As três libs dedicadas não coordenam réplicas com segurança (lock sem filtro, ou nenhum lock).

## 4. Layout de Pacotes

```
internal/kernel/
  unitofwork.go             # NOVO: porta UnitOfWork
  events/
    event.go                # Event (inalterado)
    dispatcher.go           # REMOVIDO
    envelope.go             # NOVO: Envelope (contrato entregue ao assinante)

internal/platform/
  db/
    ambient_driver.go       # NOVO: dialect.Driver que usa a tx do ctx
    unit_of_work.go         # NOVO: implementação de kernel.UnitOfWork
    transaction_context.go  # NOVO: chave do ctx, WithTransaction/TransactionFrom
  outbox/                   # NOVO (substitui platform/events)
    recorder.go             # grava eventos como jobs do River, exige tx no ctx
    mapper.go               # registro nome-do-evento → mapper
    subscriptions.go        # registro nome-do-evento → assinantes nomeados
    delivery_worker.go      # river.Worker que roteia o job ao assinante
  events/                   # REMOVIDO (InProcessDispatcher)

internal/financialtracking/adapter/eventmapper/   # NOVO: um mapper por evento do BC
internal/identity/adapter/eventmapper/            # NOVO: mapper do Registered

migrations/river/           # NOVO: SQL do River extraído com `river migrate-get`
```

Regras de dependência inalteradas: `domain` não importa nada fora dele; `kernel` não importa BC; `platform/outbox` não importa BC (os mappers são registrados no composition root).

## 5. Atomicidade

### Porta (kernel)

```go
type UnitOfWork interface {
    Run(ctx context.Context, work func(ctx context.Context) error) error
}
```

Semântica `Required`: se o ctx já carrega tx, `Run` se junta a ela em vez de abrir outra.

### Implementação (platform/db)

- `PostgresUnitOfWork.Run` abre `*sql.Tx`, guarda no ctx com chave não exportada, roda `work`, commita; `defer Rollback()` cobre erro e panic.
- `AmbientDriver` implementa `dialect.Driver`: em cada `Exec`/`Query`, usa `entsql.Conn{tx}` se há tx no ctx, senão o driver do pool. `Tx(ctx)` devolve `dialect.NopTx(d)` quando já há tx — é isso que faz o `withTx` do `CategoryRepository` e os `UpdateOne` internos do Ent se juntarem à tx externa.
- Os dois `NewClient` (FT e Identity) passam a montar o client sobre `AmbientDriver`. Os clients continuam de longa duração.

### Repositórios gravam os eventos

`Save` de `Account`, `Transaction` e `User` (o `Category` não emite eventos; o `Save` dele só passa a usar o `UnitOfWork` no lugar do `withTx`):

```
withTx(ctx) {
    upsert do aggregate
    recorder.Record(ctx, aggregate.Events()...)
}
aggregate.ClearEvents()   // só depois do commit bem-sucedido
```

`withTx` é o helper atual, que abre tx própria fora de um UoW e se junta à externa dentro de um. O `outbox.Recorder` recusa rodar sem tx no ctx (`ErrNoTransaction`): gravar evento em autocommit, separado do aggregate, é exatamente o bug que o outbox existe para impedir.

Dentro de um UoW, o "commit" do `withTx` é no-op; se o UoW fizer rollback depois, os eventos já foram limpos do aggregate, mas o aggregate também é descartado — nada fica inconsistente.

### Use cases

- `CategorizeTransaction` e `SignIn`: perdem o `dispatcher` e o `dispatchAndClear`. Só `Save`.
- `ImportFromProvider`: busca transações e saldo no provider PRIMEIRO, sem tx; depois `uow.Run(ctx, grava N transações + aplica saldo + salva conta)`. Uma falha no meio desfaz tudo.
- O helper `dispatchAndClear` e a interface `recorder` local saem de `application`.

### Armadilhas registradas

- Erro engolido dentro do UoW commita escrita parcial: o erro precisa chegar ao `Run`.
- `*sql.Tx` não é seguro para uso concorrente: nada de fan-out (errgroup) com ctx que carrega tx.
- `ent.Tx.OnCommit` dispararia no no-op commit; o projeto não usa hooks de commit.

## 6. Contrato de evento

```go
type Envelope struct {
    ID         uuid.UUID       // v7, gerado no Record; chave de dedup do consumidor
    Name       string          // ex.: "financialtracking.transaction.categorized" (as constantes EventType* que já existem)
    Version    int             // versão do payload
    OccurredAt time.Time
    Payload    json.RawMessage
}
```

- **Mapper por evento** (`EventMapper`: `EventName() string`, `Version() int`, `ToPayload(events.Event) (json.RawMessage, error)`, construído pelo genérico `outbox.MapperFor[E, P]`; a coleção `outbox.Mappers.Seal` gera o envelope), um arquivo por evento em `adapter/eventmapper` de cada BC. Os 5 eventos atuais: `TransactionImported`, `TransactionCategorized`, `TransactionReconciled`, `AccountBalanceUpdated`, `Registered`.
- Payload usa tipos primitivos e nomes em snake_case; identidades como string; dinheiro como centavos + moeda.
- Evento sem mapper registrado é erro no `Record` (falha no teste, não em produção silenciosa).
- `Registered` continua sem o subject do provedor (decisão de 2026-08-26).

## 7. Entrega: River como fila

- **Um job kind só**: `outbox_delivery`, args `{subscriber, event_id, event_name, event_version, occurred_at, payload}`. Único por `(subscriber, event_id)` (`ByArgs` com `river:"unique"` nesses dois campos), então enfileirar a mesma entrega duas vezes é no-op.
- Os args ficam em `jsonb`: o assinante recebe o payload com o mesmo conteúdo, mas sem garantia de ordem de chaves nem espaçamento. Consumidor compara JSON por conteúdo, nunca por bytes.
- **Fan-out no `Record`**: para cada assinante registrado no nome do evento, um job (`InsertManyTx` na `*sql.Tx` do ctx). Evento sem assinante não gera job.
- **`DeliveryWorker`** (`river.Worker[DeliveryArgs]`) resolve o assinante pelo nome e chama `Handle(ctx, Envelope)`. Assinante desconhecido no momento da entrega → `river.JobCancel` (assinante foi removido do código).
- **Assinante** tem nome estável (ex.: `log.domain_event`): é parte do args e da unicidade; renomear vira assinante novo.
- **Config do client**: `PollOnly: true`, fila `default` com `MaxWorkers` configurável (default 10), `MaxAttempts` default do River (25), retry policy default (`attempt^4` ±10%), `JobTimeout` 30s, retenção default (24h completed, 7d discarded).
- **Garantia**: at-least-once. Consumidor deduplica por `(Envelope.ID, subscriber)`.
- O River fica atrás de `platform/outbox`; nada em `domain`, `kernel` ou `application` importa River.

## 8. Migrations

- O SQL do River é extraído com `river migrate-get --all --up` para `migrations/river/` e aplicado pelo Atlas, como os outros BCs. Nada de `rivermigrate` no boot: forward-only e um dono só das migrations.
- Novo alvo `make migrate-river-get` documenta a extração; `migrate-apply` passa a aplicar os três diretórios.
- Atualização de versão do River exige extrair a migration nova (registrado no README).

## 9. Composition Root e ciclo de vida

- `main.go` monta `AmbientDriver` → clients Ent → `UnitOfWork` → registros de mapper e assinantes → `outbox.Recorder` (injetado nos repositórios) → `river.Client`.
- `errgroup` com o ctx do sinal: um goroutine roda o servidor HTTP, outro o `river.Client.Start`.
- Shutdown: para o servidor HTTP (`Shutdown` com timeout), depois `riverClient.Stop(ctx)` com o mesmo prazo, caindo para `StopAndCancel` se estourar. Job interrompido volta pelo rescuer do River.
- Config nova: `OUTBOX_MAX_WORKERS`, `OUTBOX_JOB_TIMEOUT` (com defaults).

## 10. Estratégia de Testes

Unitários (`make test`):
- Use cases sem dispatcher; `ImportFromProvider` com fake de `UnitOfWork` (`return work(ctx)`) e teste de que o provider é chamado ANTES do `Run`.
- Cada mapper: payload esperado em JSON (golden inline).
- `DeliveryWorker`: roteia para o assinante certo; assinante desconhecido cancela.

Integração (tag `integration`, Postgres real):
- `Save` de cada aggregate grava aggregate + job na mesma tx; falha no upsert não grava job.
- `Recorder` fora de tx devolve `ErrNoTransaction`.
- `ImportFromProvider` falhando no meio: nenhuma transação, nenhum job, saldo intacto.
- Os dois clients Ent (FT e Identity) dentro de um mesmo `Run` commitam e fazem rollback juntos.
- Entrega ponta a ponta com `river.Client` real: evento categorizado chega ao assinante; assinante que falha é reagendado; dois assinantes, um falhando, o outro não é reexecutado.
- Mesmo evento gravado duas vezes gera um job só.

Testes novos em inglês, com subtestes `t.Run("should ...")`. Os arquivos de teste existentes estão em português (`TestUserResolverProvisionaEDevolveUUIDLocal`); arquivo de teste tocado por este PR é convertido inteiro na mesma mudança.

## 11. Histórico

| Data | Mudança |
|---|---|
| 2026-11-04 | Versão inicial. Desenho fechado após pesquisa em código (Three Dots Labs, go-ddd, trm, kratos, Ent, River, watermill-sql e libs de outbox). Duas revisões em relação à primeira proposta: (1) Unit of Work deixou de ser o caminho único — o repositório grava aggregate + eventos, e o UoW fica para o use case com vários aggregates; (2) o worker próprio com tabelas `outbox_events`/`outbox_deliveries` foi trocado pelo River, com um job por (evento, assinante). |
| 2026-11-04 | Etapa 3. Unicidade do job restrita a `(subscriber, event_id)`. Registrado que o payload passa por `jsonb`. `Category` fora da lista de aggregates com eventos. Escrita do aggregate + eventos encapsulada em `outbox.AggregateWriter`. Testes de repositório provam a atomicidade com uma fila de prova na mesma tx; a fila real do River é provada no pacote `outbox`, num schema próprio. |

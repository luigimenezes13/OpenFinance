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
| D1 | **O use case é dono da transação**, via `kernel.TransactionRunner.Begin`. O repositório só persiste. | Fronteira de transação é decisão de aplicação (Evans, Vernon) e é o mesmo desenho do `TransactionRunner` do `mars-billing`. Com o repositório dono da tx, o `Save` acumulava três motivos para mudar (mapeamento, política de evento, transação) e o use case escondia que havia transação. |
| D2 | **Todo use case de escrita usa o `Begin`**, inclusive os de um aggregate só. O use case entrega os aggregates com `RecordEventsOf`; o runner grava os eventos no outbox antes do commit e os limpa depois. | Um caminho só, sem exceção para o `ImportFromProvider`. Gravar vários aggregates numa unidade é decisão explícita do use case (renegociação, importação); o padrão continua sendo um aggregate por transação e reação por evento. |
| D3 | **A tx do UoW viaja no `context`**, e os clients Ent a encontram via um driver próprio (`AmbientDriver`) | Cada BC tem seu client Ent gerado; `ent.NewTxContext` é por package e não conecta os dois. Montar client sobre `*sql.Tx` dá panic em `UpdateOne` e `Tx()`. |
| D4 | **Portas `UnitOfWork` e `events.Outbox` e o `TransactionRunner` no kernel** (`internal/kernel`) | Regra do projeto: port é domínio. O runner não tem infra (só as duas portas) e é usado pelos dois BCs; na billing ele vive em `app/services` porque lá há um BC só. |
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
- **`mars-billing` (V4)** — `src/app/services/TransactionRunner.ts`: o use case abre `begin(repos, work)`, o `context` coleta eventos com `publishAfterCommit` e o runner publica no RabbitMQ depois do commit; `TransactionAlreadyStartedException` em vez de aninhar. Adotado o desenho; a diferença é que aqui os eventos vão para o outbox **antes** do commit, o que fecha a perda de evento quando o processo cai ou o broker falha depois do commit.
- **watermill-sql, pkritiotis/go-outbox, pgx-outbox, oagudo/outbox** — referências secundárias. As três libs dedicadas não coordenam réplicas com segurança (lock sem filtro, ou nenhum lock).

## 4. Layout de Pacotes

```
internal/kernel/
  unitofwork.go             # NOVO: porta UnitOfWork
  transaction_runner.go     # NOVO: TransactionRunner (fronteira de transação do use case)
  events/
    event.go                # Event (inalterado)
    dispatcher.go           # REMOVIDO
    envelope.go             # NOVO: Envelope (contrato entregue ao assinante)
    outbox.go               # NOVO: porta Outbox (Record dentro da tx)
    subscriber.go           # NOVO: porta Subscriber

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
- `AmbientDriver` implementa `dialect.Driver`: em cada `Exec`/`Query`, usa `entsql.Conn{tx}` se há tx no ctx, senão o driver do pool. `Tx(ctx)` devolve `dialect.NopTx(d)` quando já há tx — é isso que faz o `UnitOfWork.Run` do `CategoryRepository` e os `UpdateOne` internos do Ent se juntarem à tx externa.
- Os dois `NewClient` (FT e Identity) passam a montar o client sobre `AmbientDriver`. Os clients continuam de longa duração.

### O use case abre a transação

```go
err := u.transactionRunner.Begin(ctx, func(ctx context.Context, transactionContext kernel.TransactionContext) error {
    if err := u.transactions.Save(ctx, target); err != nil {
        return err
    }
    transactionContext.RecordEventsOf(target)
    return nil
})
```

- `Begin` roda o `work` dentro do `UnitOfWork`. Leituras e locks feitos com o `ctx` de dentro usam a mesma tx (o `AmbientDriver` resolve), então um `SELECT ... FOR UPDATE` segura até o commit.
- Quando o `work` retorna sem erro, o runner lê os eventos de cada aggregate entregue e chama `events.Outbox.Record` **antes** do commit. Evento levantado depois do `RecordEventsOf` entra; o mesmo aggregate entregue duas vezes conta uma.
- Só depois do commit o runner chama `ClearEvents()`. Falha no `work`, no outbox ou no commit deixa os eventos no aggregate.
- `Begin` dentro de `Begin`, ou dentro de um `UnitOfWork.Run` já aberto (`UnitOfWork.InTransaction`), devolve `ErrTransactionAlreadyStarted`: o interno se juntaria à tx de fora e limparia os eventos antes do commit dela, e um rollback de fora os perderia. O `UnitOfWork` continua `Required`: é o que deixa o `CategoryRepository` gravar categoria + regras numa tx própria quando chamado fora de um `Begin`.
- O `outbox.Recorder` implementa `events.Outbox` e recusa rodar sem tx no ctx (`ErrNoTransaction`): esquecer o `Begin` vira erro, não evento perdido.
- Esquecer o `RecordEventsOf` grava o aggregate sem os eventos. A proteção é teste: cada use case que levanta evento tem caso conferindo o que chegou no outbox fake.

### Repositórios

`Save` de `Account`, `Transaction` e `User` volta a ser só o upsert. O `Category` mantém o `UnitOfWork` para gravar categoria e regras juntas.

### Use cases

- Todos os de escrita (`CreateAccount`, `RenameAccount`, `CreateCategory`, `RenameCategory`, `MoveCategory`, `AddCategoryRule`, `RemoveCategoryRule`, `RecordTransaction`, `CategorizeTransaction`, `ImportFromProvider`, `SignIn`) recebem o `TransactionRunner` e gravam dentro do `Begin`.
- `ImportFromProvider`: busca transações e saldo no provider PRIMEIRO, sem tx; depois um `Begin` grava N transações + conta e entrega todos ao `RecordEventsOf`. Uma falha no meio desfaz tudo.
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
- **`DeliveryWorker`** (`river.Worker[DeliveryArgs]`) resolve o assinante pelo nome e chama `Handle(ctx, Envelope)`. Assinante desconhecido no momento da entrega → erro comum, que o River tenta de novo: num rolling deploy um pod antigo pode pegar a entrega de um assinante que só a versão nova registra. Assinante removido de vez esgota as tentativas e vira `discarded`.
- **Assinante** tem nome estável (ex.: `log.domain_event`): é parte do args e da unicidade; renomear vira assinante novo.
- **Config do client**: `PollOnly: true`, fila `default` com `MaxWorkers` configurável (default 10), `MaxAttempts` default do River (25), retry policy default (`attempt^4` ±10%), `JobTimeout` 30s, retenção default (24h completed, 7d discarded).
- **Garantia**: at-least-once. Consumidor deduplica por `(Envelope.ID, subscriber)`.
- O River fica atrás de `platform/outbox`; nada em `domain`, `kernel` ou `application` importa River.

## 8. Migrations

- O SQL do River é extraído com `river migrate-get --all --up` para `migrations/river/` e aplicado pelo Atlas, como os outros BCs. Nada de `rivermigrate` no boot: forward-only e um dono só das migrations.
- Novo alvo `make migrate-river-get` documenta a extração; `migrate-apply` passa a aplicar os três diretórios.
- Atualização de versão do River exige extrair a migration nova (registrado no README).

## 9. Composition Root e ciclo de vida

- `main.go` monta `AmbientDriver` → clients Ent → `UnitOfWork` → registros de mapper e assinantes → `river.Client` → `outbox.Recorder` → `kernel.TransactionRunner` (injetado nos use cases de escrita).
- `errgroup` com o ctx do sinal: um goroutine roda o servidor HTTP, outro o `river.Client.Start`.
- Shutdown: para o servidor HTTP (`Shutdown` com timeout), depois `riverClient.Stop(ctx)` com o mesmo prazo, caindo para `StopAndCancel` se estourar. Job interrompido pelo `StopAndCancel` falha a tentativa e o River tenta de novo; só um processo que morre no meio deixa o job em `running`, e aí o rescuer o devolve depois de `RescueStuckJobsAfter` (1h por padrão).
- Config nova: `OUTBOX_MAX_WORKERS`, `OUTBOX_JOB_TIMEOUT` (com defaults).

## 10. Estratégia de Testes

Unitários (`make test`):
- Use cases sem dispatcher; `ImportFromProvider` com fake de `UnitOfWork` (`return work(ctx)`) e teste de que o provider é chamado ANTES do `Run`.
- Cada mapper: payload esperado em JSON (golden inline).
- `DeliveryWorker`: roteia para o assinante certo; assinante desconhecido é tentado de novo, não cancelado.

Integração (tag `integration`, Postgres real):
- `TransactionRunner.Begin` grava aggregates + jobs na mesma tx; falha no meio não grava nenhum dos dois e mantém os eventos no aggregate.
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
| 2026-11-06 | Etapa 5. Assinante de log é o `outbox.DomainEventLog` (`log.domain_event`). O River sobe com `context.WithoutCancel`: cancelar o ctx do `Start` é parada brusca, então o sinal só dispara o `Stop` gracioso. O HTTP para antes do worker, para nenhuma requisição gravar evento depois que o worker saiu. Reprocessamento de `discarded` documentado no README por SQL (precisa zerar `finalized_at`). |
| 2026-11-06 | Etapa 6. Os testes que precisam dos dois BCs e do River ao mesmo tempo (importação que falha no meio, `Run` com os dois clients Ent) ficam em `internal/integrationtest`, num schema próprio com as três pastas de migration aplicadas, para não colidir com os harnesses por BC que rodam em paralelo. |
| 2026-11-07 | Etapa 7, depois da review do desenho. D1 e D2 invertidas: a fronteira de transação sai do `Save` do repositório e vai para o use case, via `kernel.TransactionRunner`, no desenho do `TransactionRunner` do `mars-billing`. `outbox.AggregateWriter` removido; `Recorder` passa a implementar a porta `events.Outbox`. `Begin` aninhado é recusado. |
| 2026-11-07 | Etapa 8, depois do code review. Entrega para assinante desconhecido é tentada de novo em vez de cancelada (rolling deploy). `Begin` também recusa rodar dentro de um `UnitOfWork.Run` aberto, via `UnitOfWork.InTransaction`. Docs do `EventRecorder` em inglês e corrigidas para o runner; comentário do rescuer corrigido. |

# Shared Kernel + EventRecorder — Design Spec

**Data:** 2026-07-17
**Status:** Aprovado
**Linguagem-alvo:** Go 1.26+
**Projeto:** Gestor de gastos com Open Finance — extração de building blocks de domínio

> Este documento é o spec formal aprovado para implementação. Atualizações pontuais permitidas durante implementação só com nota no histórico. Mudanças de escopo exigem novo spec.

---

## Sumário

1. [Motivação](#1-motivação)
2. [Decisões e Não-Objetivos](#2-decisões-e-não-objetivos)
3. [Layout de Pacotes](#3-layout-de-pacotes)
4. [EventRecorder](#4-eventrecorder)
5. [Migração dos Aggregates](#5-migração-dos-aggregates)
6. [Estratégia de Testes](#6-estratégia-de-testes)
7. [Gancho para Event Sourcing](#7-gancho-para-event-sourcing)
8. [Histórico](#8-histórico)

---

## 1. Motivação

Ao modelar o segundo aggregate (`Transaction`), a gestão de eventos de domínio virou **copy-paste puro**: o campo `events []events.Event`, o `Events()` com `slices.Clone` e o `ClearEvents()` são **idênticos** em `Account` e `Transaction`. Cada aggregate novo repete o mesmo bloco.

O objetivo é extrair esse boilerplate para um building block reutilizável — no espírito do que o `mars-api` (TypeScript) faz com sua base `Entity`/`AggregateRoot`, **mas idiomático em Go**: composição via *struct embedding*, não herança; peças pequenas, sem framework, sem reflection, sem codegen.

### O que NÃO é boilerplate (e por isso fica como está)

O "trio de identidade" de cada aggregate (`xIdentity struct{}` + `NewXID()` + `XIDFromUUID()`) **parece** repetição, mas ~80% dele é decisão deliberada de domínio:

- `XIDFromUUID` traduz o erro genérico (`ErrInvalidIdentifier`) para o sentinel do BC (`ErrInvalidID`) — erro tipado é segurança, não ruído.
- `NewAccountID()` é linguagem ubíqua; `NewTypedID[accountIdentity]()` não é.

A base genérica pesada (`TypedID`, `NewTypedID`, `TypedIDFromUUID`) **já existe** e já resolve o trabalho real. Abstrair além disso esconderia os erros tipados. **YAGNI: não mexer no trio.**

---

## 2. Decisões e Não-Objetivos

### Decisões

| # | Decisão | Razão |
|---|---|---|
| D1 | **Kit thin por composição**, não framework rico estilo `mars-api` | Herança/decorators não existem em Go; base-class gorda é anti-idiomática. |
| D2 | **Novo pacote cross-BC `internal/kernel`** (Shared Kernel) | Casa reutilizável pelos 4 BCs futuros. "Shared Kernel" é termo de DDD (Evans) — nome ubíquo, não técnico. |
| D3 | O único artefato novo é o **`EventRecorder`** | É a única duplicação real digna de extração. |
| D4 | Aggregate **embeda** `kernel.EventRecorder`; `RecordEvent` fica **público e promovido** | Zero boilerplate. Trade-off aceito conscientemente (ver D5). |
| D5 | A invariante "só o root emite eventos" passa a ser garantida por **convenção + barreira `internal/`**, não pelo compilador | Escolha pragmática do dono do projeto. O ponto fraco (código externo *poderia* chamar `RecordEvent`) foi apresentado e aceito. |

### Não-objetivos (YAGNI explícito)

| Item | Por que não entra agora |
|---|---|
| **Event Sourcing** | Interesse confirmado, mas denso demais para o primeiro projeto Go (puxa CQRS/read models, versionamento de evento, store, replay). Entra depois como fatia de aprendizado dedicada, em **um único aggregate** (ver §7). |
| **Abstrair o trio de identidade** | ~80% é decisão de domínio (erros tipados), não boilerplate. |
| **Mover `Money`, `Currency`, `UserID` para o kernel** | Só existe 1 BC hoje. Mover abstração cross-BC antes do 2º consumidor é adivinhação. `UserID` é o próximo candidato quando o BC #2 nascer. |
| **`Guard`/helpers de validação** | O código já usa guard clauses explícitas (idiomático). O próprio `mars-api` não tem `Guard`. |
| **Envelope rico de evento** (id, version, groupId como no `mars-api`) | A interface `Event` mínima atual (`EventName`, `OccurredAt`) basta para o modelo domain-events. `version` só vira necessário no dia do ES. |

---

## 3. Layout de Pacotes

Novo pacote `internal/kernel`. Depende **só** de stdlib + `google/uuid`. **Nenhum import de BC** — a seta de dependência é sempre `financialtracking → kernel`, nunca o contrário.

```
internal/kernel/
  events/
    event.go        # Event (interface)          ← movido de financialtracking/domain/shared/events/event.go
    dispatcher.go   # Dispatcher, Handler         ← movido de .../shared/events/dispatcher.go
  identity.go       # TypedID + NewTypedID + TypedIDFromUUID   ← movido de shared/identifier.go
  errors.go         # ErrInvalidIdentifier         ← movido de shared/identifier.go (onde vivia junto do TypedID)
  recorder.go       # EventRecorder               ← NOVO
```

### O que se move e o que fica

- **Move para `internal/kernel`:** contrato de eventos (`Event`, `Dispatcher`, `Handler`), `TypedID` + construtores, `ErrInvalidIdentifier`.
- **Fica em `financialtracking/domain/shared`:** `Money`, `Currency`, `UserID` e seus erros (`ErrInvalidUserID`, `ErrInvalidCurrency`). O `shared/errors.go` (só `ErrForbidden`) não é tocado — `ErrInvalidIdentifier` vivia em `shared/identifier.go` e migra junto com o `TypedID`, virando `kernel/errors.go`.
- **Ajuste de imports:** ~10 arquivos do `financialtracking` trocam `.../domain/shared` e `.../domain/shared/events` por `.../kernel` e `.../kernel/events`. São **apenas mudanças de import path — nenhuma alteração de lógica.**

### Restrição de dependência (por que o contrato de evento move junto)

O `EventRecorder` guarda `[]events.Event`, logo importa o pacote `events`. Se o `EventRecorder` (em `kernel`) importasse um `events` que vivesse dentro de `financialtracking`, o Shared Kernel dependeria de um BC — seta invertida. Por isso o contrato de evento **tem** que morar no `kernel`.

---

## 4. EventRecorder

```go
package kernel

import (
	"slices"

	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

// EventRecorder é o building block embedável que centraliza o acúmulo de
// eventos de domínio de um aggregate root. Elimina a repetição de
// events []events.Event + Events() + ClearEvents() em cada root.
type EventRecorder struct {
	events []events.Event
}

// RecordEvent anexa um evento ao histórico do aggregate. Por convenção,
// chamado APENAS de dentro do root (métodos de mutação). O embedding
// promove este método como público — a barreira internal/ + a convenção
// protegem a invariante "só o root emite eventos".
func (r *EventRecorder) RecordEvent(event events.Event) {
	r.events = append(r.events, event)
}

// Events retorna CÓPIA dos eventos acumulados — expor o slice interno
// deixaria o chamador mutar o histórico por fora do root. Lido pelo use
// case após persistir: Save → Dispatch → ClearEvents.
func (r *EventRecorder) Events() []events.Event {
	return slices.Clone(r.events)
}

// ClearEvents descarta os eventos já despachados.
func (r *EventRecorder) ClearEvents() {
	r.events = nil
}
```

O zero value é utilizável (slice `nil`) — nenhum construtor precisa inicializar o `EventRecorder`.

---

## 5. Migração dos Aggregates

`Account` e `Transaction` recebem a mesma transformação. Exemplo com `Account`:

**Antes:**
```go
type Account struct {
	id AccountID
	// ...
	events []events.Event
}
func (a *Account) Events() []events.Event { return slices.Clone(a.events) }
func (a *Account) ClearEvents()           { a.events = nil }
// em UpdateBalance:
a.events = append(a.events, NewBalanceUpdated(a.id, a.userID, previous, newBalance))
```

**Depois:**
```go
type Account struct {
	kernel.EventRecorder // embed → promove Events(), ClearEvents(), RecordEvent()
	id AccountID
	// ...
}
// Events() e ClearEvents() são REMOVIDOS (promovidos do embed)
// em UpdateBalance:
a.RecordEvent(NewBalanceUpdated(a.id, a.userID, previous, newBalance))
```

O **lifecycle não muda**: o use case continua `Save → Events() → Dispatch → ClearEvents()`. É refactor comportamentalmente idêntico.

---

## 6. Estratégia de Testes

- **`recorder_test.go` (novo):** grava evento → `Events()` devolve o esperado; mutar o slice retornado **não** afeta o interno (prova do `slices.Clone`); `ClearEvents()` zera; zero value é usável sem init.
- **`identity_test.go` (kernel):** o `TypedID` ganha teste próprio ao migrar (hoje é testado só indiretamente via `UserID`/`AccountID`).
- **Rede de não-regressão:** `account_test.go` e `transaction_test.go` existentes **continuam passando sem alteração** — é a garantia de que a migração preserva comportamento. Se algum quebrar, a migração mudou semântica e precisa revisão.

---

## 7. Gancho para Event Sourcing

O `EventRecorder` já é o "tijolo" compartilhado dos dois mundos. No dia do spike de ES (fatia de aprendizado, **um** aggregate escolhido, resto state-stored):

- Aquele root **adiciona** `apply(event)` / `when(event)` para reconstruir estado.
- Um event store passa a reconstruir o aggregate por replay.
- `Events()` vira exatamente "mudanças não-commitadas a anexar no store".

Encaixa sem retrabalho no `EventRecorder`. **Não é escopo desta spec.**

---

## 8. Histórico

| Data | Mudança |
|---|---|
| 2026-07-17 | Versão inicial aprovada. |

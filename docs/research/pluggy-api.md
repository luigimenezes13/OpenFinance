# Pluggy API — Referência técnica para o adapter Go

**Data da pesquisa:** 2026-06-22
**Propósito:** fundamentar o Bounded Context "Open Finance Integration" e o port `OpenFinanceProvider` no PR4. Foco: o que um adapter Go precisa entender da Pluggy antes de escrever a primeira linha de HTTP client.

**Fontes consolidadas** (citadas por seção mais abaixo):
- https://docs.pluggy.ai/
- https://docs.pluggy.ai/docs/authentication
- https://docs.pluggy.ai/docs/item
- https://docs.pluggy.ai/docs/item-lifecycle
- https://docs.pluggy.ai/docs/accounts
- https://docs.pluggy.ai/docs/transactions
- https://docs.pluggy.ai/docs/credit-card-bills
- https://docs.pluggy.ai/docs/investments
- https://docs.pluggy.ai/docs/identities
- https://docs.pluggy.ai/docs/connectors-coverage
- https://docs.pluggy.ai/docs/webhooks
- https://docs.pluggy.ai/docs/rate-limits
- https://docs.pluggy.ai/docs/server-side-sdks
- https://docs.pluggy.ai/reference (OAS em https://api.pluggy.ai/oas3.json)

---

## 1. Conceitos centrais

A Pluggy modela a integração com instituições financeiras numa hierarquia bem definida: **Connector** (a instituição), **Item** (uma conexão de um usuário com aquela instituição) e os **produtos** desse Item (Accounts, Transactions, Credit Card Bills, Investments, Identity). Cada produto é um sub-recurso ligado ao Item.

### 1.1 Connector

Representa a integração com uma instituição financeira (Nubank, Itaú, BB, Inter, XP, Wise etc.). É um **catálogo estático mantido pela Pluggy** — o cliente da API não cria connectors, apenas lista.

| Campo | Tipo | Observação |
|---|---|---|
| `id` | number (int) | Identidade. Diferente de quase todos os outros recursos, NÃO é UUID — é inteiro. |
| `name` | string | "Itaú", "Nubank", etc. |
| `institutionUrl` | string | Site da instituição |
| `imageUrl` | string | Logo |
| `primaryColor` | string | Hex sem `#` |
| `type` | enum | `PERSONAL_BANK`, `BUSINESS_BANK`, `INVESTMENT`, `OTHER` |
| `country` | string ISO | `BR`, `AR`, etc. |
| `credentials` | array | Schema dos campos de login que a Pluggy vai pedir |
| `products` | array | Quais produtos esse connector entrega (`ACCOUNTS`, `TRANSACTIONS`, `CREDIT_CARDS`, `INVESTMENTS`, `IDENTITY`, `LOANS`, `PAYMENTS`) |
| `hasMFA` | boolean | Se exige autenticação multi-fator |
| `isSandbox` | boolean | Conector fake para testes |
| `health` | object | Status operacional do connector — o canal estável para reagir a mudanças é o webhook `connector/status_updated` |

Fonte: https://docs.pluggy.ai/docs/connectors-coverage

### 1.2 Item

O conceito **central** da Pluggy. É uma conexão ativa entre um usuário e um connector — guarda credenciais (ou tokens OAuth no caso Open Finance Regulado) e é a porta de entrada para todos os produtos daquele vínculo.

- **Identidade**: UUID v4 (string).
- **Ciclo de vida**: criado via widget ou API → sync inicial puxa até 365 dias de histórico → auto-sync recorrente (8h/12h/24h, feature paga) → eventualmente deletado pelo cliente, por inatividade no sandbox (30 dias), ou por deprecação do connector (janela de 30 dias).
- Cada produto subordinado (Account, Transaction, etc.) referencia o Item via `itemId`.

#### Status do Item (`status` — visão de alto nível)

| Status | Significado | Ação do integrador |
|---|---|---|
| `UPDATING` | Sync em andamento | Aguardar webhook `item/updated` ou fazer polling com backoff |
| `UPDATED` | Sync concluído com sucesso | Ler accounts/transactions |
| `OUTDATED` | Sync terminou com erro inesperado | Inspecionar `executionStatus`; pode tentar de novo |
| `LOGIN_ERROR` | Credenciais inválidas; auto-sync pausado | Pedir reconexão pro usuário (gerar Connect Token de update) |
| `WAITING_USER_INPUT` | Aguarda MFA/2FA | Abrir o widget no modo MFA ou bater `POST /items/{id}/mfa` |

#### Execution Status (`executionStatus` — visão fina do step atual)

Estados transitórios (sync rodando):
- `CREATED`
- `LOGIN_IN_PROGRESS` (pode levar até ~5min)
- `LOGIN_MFA_IN_PROGRESS`
- `ACCOUNTS_IN_PROGRESS`
- `CREDITCARDS_IN_PROGRESS`
- `TRANSACTIONS_IN_PROGRESS`
- `INVESTMENT_TRANSACTIONS_IN_PROGRESS`
- `PAYMENT_DATA_IN_PROGRESS`
- `IDENTITY_IN_PROGRESS`
- `MERGING` (validando/persistindo)

Estados finais de sucesso:
- `SUCCESS` — todos os produtos coletados
- `PARTIAL_SUCCESS` — alguns produtos falharam; ver `statusDetail`

Estados finais de erro (todos terminais, exigem ação):
- `ERROR`, `MERGE_ERROR`
- `INVALID_CREDENTIALS`, `INVALID_CREDENTIALS_MFA`
- `ALREADY_LOGGED_IN`
- `SITE_NOT_AVAILABLE`
- `USER_INPUT_TIMEOUT`
- `ACCOUNT_LOCKED`, `ACCOUNT_NEEDS_ACTION`, `ACCOUNT_CREDENTIALS_RESET`
- `USER_NOT_SUPPORTED`
- `CONNECTION_ERROR`
- `USER_AUTHORIZATION_NOT_GRANTED`, `USER_AUTHORIZATION_REVOKED`

Estados intermediários que pausam o fluxo aguardando o usuário:
- `WAITING_USER_INPUT` — login deu certo, falta um input (MFA)
- `USER_AUTHORIZATION_PENDING` — usuário precisa autorizar fora da plataforma (típico de Open Finance Regulado); Pluggy retoma sozinha

Fonte: https://docs.pluggy.ai/docs/item-lifecycle

### 1.3 Account

Conta financeira (BANK ou CREDIT) pertencente a um Item.

| Campo | Tipo | Observação |
|---|---|---|
| `id` | UUID | Identidade |
| `type` | enum | `BANK` \| `CREDIT` |
| `subtype` | enum | `CHECKING_ACCOUNT`, `SAVINGS_ACCOUNT`, `CREDIT_CARD` |
| `number` | string | `0001/12345-0` para banco; últimos 4 dígitos no cartão |
| `name` | string | "Conta Corrente" |
| `marketingName` | string | Tier comercial (opcional) |
| `balance` | number | Saldo (banco) OU fatura atual em aberto (cartão) |
| `currencyCode` | string ISO | `BRL`, `USD` |
| `owner` | string | Titular |
| `taxNumber` | string | CPF/CNPJ |
| `itemId` | UUID | FK pro Item |
| `bankData` | object | Só para `BANK`: transferNumber, closingBalance, automaticallyInvestedBalance, overdraftContractedLimit, overdraftUsedLimit |
| `creditData` | object | Só para `CREDIT`: minimumPayment, creditLimit, availableCreditLimit, balanceCloseDate, balanceDueDate, brand, level, holderType (`MAIN` \| `ADDITIONAL`), status |

Fonte: https://docs.pluggy.ai/docs/accounts

### 1.4 Transaction

Movimentação individual de uma Account. **Recurso mais volumoso** e o mais delicado para idempotência (ver §5).

Resumo (detalhe completo em §5):
- `id` UUID — **NÃO é estável**; muda se `date`, `description` ou `amount` mudarem materialmente
- `type` enum `DEBIT` \| `CREDIT`
- `amount` decimal — sinal varia conforme conta corrente vs cartão (ver §5)
- `status` enum `PENDING` \| `POSTED`
- `category` string (Pluggy enrichment, tier Pro)
- `providerCode` string — código original da instituição (mais estável que `id` em alguns connectors)

Fonte: https://docs.pluggy.ai/docs/transactions

### 1.5 Credit Card Bill

Fatura mensal do cartão. Agregado de transações + encargos + pagamentos.

| Campo | Tipo |
|---|---|
| `id` | UUID |
| `accountId` | UUID (FK pra Account `CREDIT`) |
| `dueDate` | string date |
| `totalAmount` | number |
| `totalAmountCurrencyCode` | string ISO |
| `minimumPaymentAmount` | number (opcional) |
| `allowsInstallments` | boolean |
| `financeCharges` | array (juros, IOF, multas) |
| `payments` | array (pagamentos realizados) |

Obrigatório em todos os connectors Open Finance Regulado.

Fonte: https://docs.pluggy.ai/docs/credit-card-bills

### 1.6 Investment

Posição em ativos (CDB, LCI, Tesouro, ações, FIIs, fundos, ETFs, COE, previdência).

Tipos principais (`type`):
- `FIXED_INCOME` — CDB, LCI/LCA, Tesouro, debêntures
- `EQUITY` — ações, FIIs, opções, derivativos
- `MUTUAL_FUND` — fundos
- `ETF`
- `SECURITY` — PGBL, VGBL, previdência
- `COE`

Campos relevantes: `id` UUID, `itemId`, `code`, `isin` (12 chars, identificador global), `balance`, `quantity`, `value`, `amount`, `annualRate`, `lastMonthRate`, `lastTwelveMonthsRate`, `taxes`, `rate`, `rateType` (CDI/SELIC/IPCA), `fixedAnnualRate`, `dueDate`, `status` (`ACTIVE`, `PENDING`, `TOTAL_WITHDRAWAL`).

Investment transactions são separadas das transactions normais — representam aplicações/resgates, não gastos do dia a dia.

Fonte: https://docs.pluggy.ai/docs/investments

### 1.7 Identity

Dados pessoais do titular. Em Open Finance Regulado vem bem rico (perfil de investidor, qualificações, sociedades).

Campos núcleo: `id`, `itemId`, `fullName`, `companyName`, `document`, `documentType` (CPF/CNPJ), `birthDate`, `jobTitle`, `emails[]`, `phoneNumbers[]`, `addresses[]`, `relations[]`.

Fonte: https://docs.pluggy.ai/docs/identities

---

## 2. Fluxo de conexão (Consent Flow)

A Pluggy separa estritamente o que vive no servidor (segredo) do que vive no frontend (token de curta duração com escopo reduzido).

### 2.1 Atores e segredos

| Lugar | Credencial | Validade | Escopo |
|---|---|---|---|
| Backend (Go adapter) | `CLIENT_ID` + `CLIENT_SECRET` | permanente | tudo |
| Backend (em memória/cache) | `apiKey` | 2 horas | full API |
| Frontend (browser/widget) | `connectToken` | 30 minutos | apenas operações de consent + leitura básica do Item gerado |

Regra de ouro: **CLIENT_ID/CLIENT_SECRET nunca saem do servidor.** O `apiKey` também não — ele só serve pro adapter Go trocar por `connectToken` quando o frontend pedir.

Fonte: https://docs.pluggy.ai/docs/authentication

### 2.2 Sequência completa (criação de um novo Item)

```
[Frontend]                    [Backend Go]                 [Pluggy API]
    |                              |                            |
    | 1. "quero conectar banco"    |                            |
    |----------------------------->|                            |
    |                              | 2. POST /auth/create       |
    |                              |   {clientId, clientSecret} |
    |                              |--------------------------->|
    |                              |          {apiKey}          |
    |                              |<---------------------------|
    |                              | 3. POST /connect_token     |
    |                              |   X-API-KEY: apiKey        |
    |                              |   {options: {              |
    |                              |     clientUserId: "user-1",|
    |                              |     webhookUrl: "...",     |
    |                              |     avoidDuplicates: true  |
    |                              |   }}                       |
    |                              |--------------------------->|
    |                              |       {accessToken}        |
    |                              |<---------------------------|
    |   4. {connectToken}          |                            |
    |<-----------------------------|                            |
    |                                                           |
    | 5. abre Pluggy Connect Widget(connectToken)               |
    |---------------------------------------------------------->|
    |   (usuário escolhe banco, digita credenciais, MFA, etc.)  |
    |                                                           |
    |   6. onSuccess({ item: { id } })                          |
    |<----------------------------------------------------------|
    |                              |                            |
    | 7. POST /api/items/sync      |                            |
    |    {itemId}                  |                            |
    |----------------------------->|                            |
    |                              | 8. persiste itemId associado|
    |                              |    ao usuário              |
    |                              |    (status inicial: UPDATING)|
    |                              |                            |
    |                              |    --- ASSÍNCRONO ---      |
    |                              |                            |
    |                              | 9a. Webhook item/updated   |
    |                              |<---------------------------|
    |                              | 9b. (ou polling GET /items/{id})
    |                              |                            |
    |                              | 10. GET /accounts?itemId=  |
    |                              |     GET /transactions...   |
    |                              |--------------------------->|
```

### 2.3 Frontend — Pluggy Connect Widget

- Distribuído como pacote npm: `pluggy-connect-sdk` (vanilla) e `react-pluggy-connect` (React).
- Recebe o `connectToken` como prop. **Nunca recebe API key.**
- Callbacks principais: `onSuccess({ item })`, `onError`, `onClose`, `onEvent`.
- O frontend só precisa enviar `item.id` pro backend. Toda lógica de negócio fica no servidor — a doc explicitamente avisa que tratar o `itemId` apenas no frontend é frágil (usuário pode fechar a aba antes do callback voltar).

Fonte: https://docs.pluggy.ai/docs/setup-pluggyconnect-widget-on-your-app

### 2.4 Fluxo de update / reconexão (LOGIN_ERROR ou MFA expirado)

1. Backend gera novo Connect Token passando `itemId` no body (modo update).
2. Frontend abre o widget com esse token — o widget reconhece e mostra direto a tela de credenciais/MFA do banco daquele Item.
3. Após sucesso, o mesmo `itemId` é reusado.

### 2.5 Conhecendo o resultado: webhook ou polling

A Pluggy recomenda webhook (`webhookUrl` é configurado no Connect Token ou no Item). Se webhook não for opção, polling `GET /items/{id}` com backoff até `status` virar `UPDATED` ou erro terminal. **Não fazer polling agressivo** — bate em rate limit (§3.11).

---

## 3. Endpoints principais do adapter Go

Base URL: `https://api.pluggy.ai`

Autenticação dos endpoints internos: header `X-API-KEY: <apiKey>` (o `apiKey` obtido em `/auth/create`). O Connect Token é usado apenas pelo widget — o adapter Go não precisa lidar com ele depois de devolvê-lo ao frontend.

### 3.1 Auth

| Método | Path | Propósito |
|---|---|---|
| `POST` | `/auth/create` | Troca `clientId` + `clientSecret` por `apiKey` (TTL 2h) |
| `POST` | `/connect_token` | Cria token público pro widget (TTL 30min); body com `options` (webhookUrl, clientUserId, oauthRedirectUri, avoidDuplicates) ou com `itemId` no modo update |

### 3.2 Connectors

| Método | Path | Propósito |
|---|---|---|
| `GET` | `/connectors` | Lista todos os connectors (filtrar por `country`, `types`, `name`) |
| `GET` | `/connectors/{id}` | Detalhe de um connector + schema de credenciais |

### 3.3 Items

| Método | Path | Propósito |
|---|---|---|
| `POST` | `/items` | Cria Item via API (alternativa ao widget; recebe `connectorId`, `parameters`, `webhookUrl`, `clientUserId`, `products`, `avoidDuplicates`) |
| `GET` | `/items/{id}` | Lê status do Item — endpoint de polling |
| `PATCH` | `/items/{id}` | Força resync; **rate-limited a 20 req/min** |
| `POST` | `/items/{id}/mfa` | Envia resposta MFA quando `status=WAITING_USER_INPUT` |
| `DELETE` | `/items/{id}` | Deleta Item + credenciais + dados (não-reversível) |

Detalhe importante de `POST /items`: o campo `parameters` (credenciais) pode ser enviado **criptografado** com RSA `RSA_PKCS1_OAEP_PADDING` + Base64. Para Open Finance Regulado o adapter normalmente nem precisa porque o fluxo é OAuth e o widget cuida. Se o adapter Go alguma vez criar Item programaticamente com credenciais (cenário sandbox/testing), implementar RSA.

### 3.4 Accounts

| Método | Path | Propósito |
|---|---|---|
| `GET` | `/accounts?itemId={uuid}` | Lista accounts do Item |
| `GET` | `/accounts/{id}` | Lê uma account específica |

### 3.5 Transactions

Atenção: **existem duas versões**.

| Método | Path | Status | Paginação |
|---|---|---|---|
| `GET` | `/transactions` | **DEPRECATED** | page-based (`page`, `pageSize` 1-500, default 500) |
| `GET` | `/v2/transactions` | atual | **cursor-based** |

Query params comuns: `accountId` (obrigatório), `from`, `to` (ISO8601 UTC), `pageSize`, `ids[]`, `billId`, `createdAtFrom`.

Para o adapter Go: **usar `/v2/transactions` desde o início** — cursor-based é mais robusto para incrementos via webhook (`transactions/created` traz `createdTransactionsLink` já apontando pra v2 com cursor).

Outros endpoints relevantes:

| Método | Path | Propósito |
|---|---|---|
| `GET` | `/transactions/{id}` | Detalhe de uma transação |
| `PATCH` | `/transactions/{id}` | Atualiza `category` manualmente |

Limits: `GET /transactions` e `GET /transactions/{id}` = **360 req/min por IP**.

### 3.6 Credit Card Bills

| Método | Path | Propósito |
|---|---|---|
| `GET` | `/bills?accountId={uuid}` | Lista faturas de uma account `CREDIT` |
| `GET` | `/bills/{id}` | Detalhe da fatura |

### 3.7 Investments

| Método | Path | Propósito |
|---|---|---|
| `GET` | `/investments?itemId={uuid}` | Lista posições |
| `GET` | `/investments/{id}` | Detalhe de uma posição |
| `GET` | `/investments/{id}/transactions` | Movimentações da posição |

Limit: 360 req/min por IP em cada um desses.

### 3.8 Identity

| Método | Path | Propósito |
|---|---|---|
| `GET` | `/identity?itemId={uuid}` | Identity do Item |
| `GET` | `/identity/{id}` | Identity por ID |

### 3.9 Webhooks (gerência)

| Método | Path | Propósito |
|---|---|---|
| `POST` | `/webhooks` | Registra URL + lista de eventos + headers customizados |
| `GET` | `/webhooks` | Lista webhooks |
| `GET` | `/webhooks/{id}` | Detalhe |
| `PATCH` | `/webhooks/{id}` | Atualiza |
| `DELETE` | `/webhooks/{id}` | Remove |

### 3.10 Paginação — resumo prático

- `/v2/transactions`: cursor. Adapter Go itera enquanto `next` (ou campo equivalente) não vier vazio. Idempotência: persistir por `id` (com cuidado — ver §5) e tolerar reentregas.
- `/accounts`, `/connectors`, `/investments`, `/bills`: tipicamente lista única (volumes baixos), mas é seguro suportar `pageSize`/`page` quando aceitos.

### 3.11 Rate limits — resumo

Per IP, **por minuto, por endpoint**:

| Endpoint | Limite |
|---|---|
| `POST /auth/create` | 360/min |
| `GET /transactions` (v1 e por id) | 360/min |
| `GET /investments` (e variantes) | 360/min |
| `PATCH /items` | **20/min** |
| Outros | não publicado |

Resposta 429:
```json
{ "message": "Too many requests. Please try again later", "code": 429 }
```
Headers: `RateLimit-Limit`, `RateLimit-Reset` (segundos até reset), `Retry-After` (sempre 60). Adapter Go deve respeitar `RateLimit-Reset` com jitter.

Fonte: https://docs.pluggy.ai/docs/rate-limits

---

## 4. Webhooks

### 4.1 Eventos disponíveis

**Data Events** (relevantes pro Open Finance Integration BC):
- `item/created`
- `item/updated`
- `item/deleted`
- `item/error`
- `item/waiting_user_input`
- `item/waiting_user_action`
- `item/login_succeeded`
- `transactions/created`
- `transactions/updated`
- `transactions/deleted`
- `connector/status_updated`

**Payment Events** (provavelmente fora do escopo PR4, mas listados pra completude):
- `payment_intent/{created,completed,error}`
- `scheduled_payment/{created,completed,error,canceled}`
- `automatic_pix_payment/{created,completed,error,canceled}`
- `smart_transfer_preauthorization/completed`
- `smart_transfer_payment/{completed,error}`

### 4.2 Envelope comum dos payloads

Todos os payloads carregam:
- `event` — nome do evento
- `eventId` — UUID único, idempotência
- `clientUserId` — o que o adapter passou em `options.clientUserId`
- `triggeredBy` — `USER` | `CLIENT` | `SYNC` | `INTERNAL`
- + campos específicos do evento

### 4.3 Exemplos concretos

**item/created** e **item/updated** (mesma forma):
```json
{
  "event": "item/updated",
  "eventId": "d876fd7c-e9bd-4c4c-bd46-cc96c62aac29",
  "itemId": "a5c763cb-0952-457b-9936-630f79c5b016",
  "triggeredBy": "USER",
  "clientUserId": "client-user-id"
}
```

**item/error**:
```json
{
  "event": "item/error",
  "eventId": "d876fd7c-e9bd-4c4c-bd46-cc96c62aac29",
  "itemId": "d161a74a-8bc8-4093-88de-724312969b0d",
  "error": {
    "code": "USER_INPUT_TIMEOUT",
    "message": "User requested input had expired",
    "parameter": "token"
  },
  "triggeredBy": "USER",
  "clientUserId": "client-user-id"
}
```

**transactions/created** (importante — traz link pré-montado pro fetch incremental):
```json
{
  "itemId": "de7bbf5a-abf2-47e4-94b1-586b36758423",
  "event": "transactions/created",
  "eventId": "4e69d62d-b7c8-4f01-b591-a1d8a94710b9",
  "accountId": "0d5a0de2-9c82-4ea2-af50-31643a632a33",
  "transactionsCount": 332,
  "transactionsMinDate": "2025-02-12T15:00:01.000Z",
  "transactionsCreatedAtFrom": "2025-02-13T17:21:53.719Z",
  "createdTransactionsLink": "https://api.pluggy.ai/transactions?accountId=..."
}
```

**transactions/updated**:
```json
{
  "event": "transactions/updated",
  "eventId": "d876fd7c-e9bd-4c4c-bd46-cc96c62aac29",
  "itemId": "a5c763cb-0952-457b-9936-630f79c5b016",
  "accountId": "8a6e2c17-2817-40bb-b03d-546febc6a60a",
  "transactionIds": ["5a14feae-eaa7-423a-820c-6b83837c35b7"]
}
```

### 4.4 Autenticação do webhook — atenção

**A Pluggy NÃO assina os payloads com HMAC.** Não existe `X-Pluggy-Signature`. A "autenticação" é feita pelo cliente registrando **headers customizados** que a Pluggy ecoa em cada requisição:

```json
POST /webhooks
{
  "url": "https://api.meudominio.com/webhooks/pluggy",
  "event": "all",
  "headers": {
    "Authorization": "Bearer <segredo_gerado_por_nos>",
    "X-Webhook-Source": "pluggy"
  }
}
```

Implicação pro adapter Go:
1. Gerar um segredo forte por ambiente, guardar em vault/env.
2. Registrar o webhook via `POST /webhooks` passando esse segredo no header.
3. No handler HTTP, **validar o header** contra o segredo esperado — sem isso, qualquer pessoa pode chamar o endpoint e injetar payloads.
4. A doc avisa que os headers só podem ser configurados via API (não pelo dashboard) porque carregam segredo.

### 4.5 Retry policy

| Tentativa | Quando |
|---|---|
| 1ª | imediato + 3 retries imediatos |
| 2ª janela | +1h, 3 tentativas |
| 3ª janela | +2h, 3 tentativas |
| **Total máximo** | **9 tentativas** |

Exceção: `item/login_succeeded` faz só 3 imediatas, sem janelas atrasadas.

Critério de sucesso: **2xx em até 5 segundos**. Se o adapter Go não conseguir processar nesse tempo, responder 200 imediatamente e enfileirar processamento assíncrono (regra padrão para webhooks).

Idempotência: usar `eventId` como chave de deduplicação na primeira coisa que o handler faz.

Fonte: https://docs.pluggy.ai/docs/webhooks

---

## 5. Modelo de Transaction em detalhe

Esta é a área mais sutil. O Bounded Context Open Finance Integration precisa traduzir esse modelo num value object de domínio próprio (ver §6).

### 5.1 Campos

| Campo | Tipo | Notas críticas |
|---|---|---|
| `id` | UUID | **Não estável** — Pluggy pode regenerar se `date`/`description`/`amount` mudarem materialmente |
| `accountId` | UUID | FK pra Account |
| `date` | ISO8601 UTC | Data de postagem (não data da operação) |
| `description` | string | Texto fornecido pela instituição (pode ser higienizado pela Pluggy) |
| `descriptionRaw` | string \| null | Texto bruto, quando disponível |
| `amount` | number (decimal) | Sinal varia — ver §5.2 |
| `currencyCode` | string ISO | `BRL`, `USD` |
| `type` | enum | `DEBIT` (saída) \| `CREDIT` (entrada) |
| `status` | enum | `PENDING` \| `POSTED` |
| `balance` | number \| null | Saldo após a transação (poucos connectors entregam) |
| `category` | string \| null | Pluggy enrichment, requer tier Pro |
| `merchant` | object \| null | Quando disponível: name, businessName, cnpj, category |
| `providerCode` | string \| null | Código do provider; **só para Open Finance Regulado + alguns connectors** |
| `providerId` | string \| null | Só para Regulado |
| `paymentData` | object \| null | Dados de PIX/TED: receiver, payer, identifier, reason, method |
| `createdAt` | ISO8601 | Quando a Pluggy ingeriu — útil para incremental sync |
| `updatedAt` | ISO8601 | Última modificação no registro |

### 5.2 Sinal de DEBIT vs CREDIT — pegadinha

Duas convenções coexistem:

**Conta corrente (`BANK`)**: o `type` carrega a direção; o `amount` é positivo (módulo).
- `type=DEBIT` → saída → modelagem de domínio: `-X`
- `type=CREDIT` → entrada → modelagem de domínio: `+X`

**Cartão de crédito (`CREDIT`)**: o `amount` já vem com sinal embutido.
- `amount` positivo → compra (aumenta dívida)
- `amount` negativo → pagamento da fatura (reduz dívida)
- O `type` ainda existe, mas a fonte de verdade econômica é o sinal de `amount` (a doc é explícita: *"For credit cards, it will be positive (debit) when its an expense (adds to the balance), while it will be negative (credit) when the person pays the bill"*).

**Implicação pro adapter Go**: o tradutor (ACL) precisa olhar `account.type` antes de decidir como normalizar o sinal. Não dá pra ter um único `if type == DEBIT` genérico.

### 5.3 Formato monetário

A API retorna **decimal JSON** (`1500`, `75.00`, `-42.31`). Não é cents. O Go adapter deve:
- Nunca usar `float64` para parse — IEEE-754 perde precisão.
- Parsear como `json.Number` ou string e converter para `int64` em centavos (ou `decimal.Decimal` de `shopspring/decimal`) **dentro do ACL**, antes de qualquer cruzamento de fronteira de domínio.
- O domínio interno deve usar Value Object `Money` com inteiro de menor unidade (ver §6).

### 5.4 Datas e timezones

- `date` é ISO8601 UTC — **mas representa a data de postagem na instituição**, que costuma estar em horário de Brasília no original. A Pluggy normaliza para UTC sem ajustar para o tipo de dia financeiro.
- Para o domínio, geralmente o que importa é o **dia local** (uma transação de 22:30 BRT num dia 5 vira `2025-05-06T01:30:00Z` em UTC — mas o usuário lê como dia 5). O ACL deve converter para `America/Sao_Paulo` e extrair `LocalDate` para exibição/agrupamento, mantendo o UTC para auditoria.

### 5.5 Idempotência — o ponto mais difícil

**Não dá para confiar 100% no `id` da Pluggy.** A doc diz literalmente que `id` pode mudar se a Pluggy detectar que o registro mudou materialmente. Isso significa que se o adapter usa só `id` como chave única, vai criar duplicatas.

Estratégias possíveis:

1. **Para Open Finance Regulado**: `providerId` (ou `providerCode` + `accountId` + `date`) é mais estável — é o identificador na origem (banco). Quando presente, é a melhor chave.

2. **Para connectors scraped (não-regulado)**: `providerCode` pode não vir. Caso geral, montar um **fingerprint** estável:
   ```
   fingerprint = sha256(accountId | date_truncated_to_day | amount_in_cents | description_normalized)
   ```
   Onde `description_normalized` é lowercase + strip + remoção de espaços duplos. Não é perfeito (duas compras idênticas no mesmo dia colidem), mas é o que dá pra fazer.

3. **Híbrido (recomendado)**: persistir `pluggy_id` + `fingerprint` + `provider_id` (nullable). Na ingestão, deduplicar por `provider_id` quando existe, senão por `fingerprint`. O `pluggy_id` fica como referência mas **não é único** no banco do domínio.

### 5.6 Paginação prática

Webhook `transactions/created` já entrega o link inicial em `createdTransactionsLink`. Para fetch manual ou backfill:

```
GET /v2/transactions?accountId={uuid}&from=2025-01-01&to=2025-06-22&pageSize=500
```

Resposta v2 traz cursor (campo `next` ou similar — confirmar no OAS quando implementar). Quando `next` vier null/vazio, terminou.

Fonte: https://docs.pluggy.ai/docs/transactions e https://docs.pluggy.ai/reference/transactions-list-1

---

## 6. Considerações para a ACL (Anti-Corruption Layer)

O Bounded Context "Open Finance Integration" não pode deixar DTOs da Pluggy vazarem para o domínio principal (Gestor de Gastos). A ACL faz a tradução. Recomendações concretas para o Go adapter:

### 6.1 Renomeação de campos (Pluggy → ubíqua de negócio)

| Pluggy | Domínio interno | Por quê |
|---|---|---|
| `Item` | `BankConnection` | "Item" é jargão Pluggy, não fala com o usuário |
| `connectorId` | `BankConnectionProviderId` ou `InstitutionCode` | "Connector" é Pluggy-speak |
| `Account` | `Account` (ok manter) | termo de negócio universal |
| `Transaction.providerCode` | `Transaction.ExternalReference` | abstrai a origem |
| `Transaction.amount` (decimal) | `Money` value object (int64 cents + currency) | precisão + tipo |
| `Transaction.type` (DEBIT/CREDIT) | `TransactionDirection` (`Inflow`/`Outflow`) | linguagem de cashflow |
| `Transaction.category` (Pluggy enrichment) | `Transaction.SuggestedCategory` (advisory) | a categorização do domínio é a fonte de verdade; a da Pluggy é só sugestão |
| `Item.status` | `BankConnection.Health` enum próprio | reduzir os ~20 valores da Pluggy a um vocabulário menor (`Healthy`, `Syncing`, `NeedsReconnect`, `Failed`) |

### 6.2 Normalização de tipos

- **Money**: decimal JSON → `int64` em centavos + `Currency` (ISO 4217). Toda matemática no domínio é com `int64`.
- **Sinal**: aplicar regra de §5.2 antes do domínio ver — domínio recebe sempre `(Direction=Outflow, Amount=positive)` ou `(Direction=Inflow, Amount=positive)`. Nunca expor "amount negativo no cartão = pagamento" para fora do ACL.
- **Datas**: converter `date` UTC para `time.Time` em `America/Sao_Paulo`, derivar `LocalDate` para a maioria das consultas. Guardar ambos.
- **IDs**: o domínio gera os próprios IDs (`TransactionId` UUID v7 ou similar). Pluggy IDs ficam num campo `ExternalIds { PluggyId, ProviderId, Fingerprint }` — todos opcionais exceto o fingerprint.

### 6.3 Tradução de erros

A Pluggy retorna ~20 `executionStatus` de erro (§1.2). O domínio quase sempre só precisa saber:

| Pluggy executionStatus | Erro de domínio |
|---|---|
| `INVALID_CREDENTIALS`, `ACCOUNT_CREDENTIALS_RESET`, `USER_AUTHORIZATION_REVOKED` | `ConsentExpired` / `NeedsReconnect` |
| `WAITING_USER_INPUT`, `USER_AUTHORIZATION_PENDING` | `AwaitingUserAction` |
| `SITE_NOT_AVAILABLE`, `CONNECTION_ERROR` | `ProviderUnavailable` (retry depois) |
| `USER_INPUT_TIMEOUT` | `MFATimeout` |
| `USER_NOT_SUPPORTED`, `ACCOUNT_LOCKED`, `ACCOUNT_NEEDS_ACTION` | `Unsupported` (final, usuário precisa agir fora) |
| `ERROR`, `MERGE_ERROR` | `TransientError` |

A função de tradução deve ser exaustiva (Go switch com `default` panic em DEV) para forçar atualização quando a Pluggy adicionar status novo.

### 6.4 Domain Events emitidos pelo BC

Sugestão de eventos que o BC Open Finance Integration publica para o resto do sistema (não vincular nada disso ao schema da Pluggy):

- `BankConnectionEstablished { connectionId, userId, institutionCode, occurredAt }`
- `BankConnectionHealthChanged { connectionId, oldHealth, newHealth, reason, occurredAt }`
- `BankConnectionRequiresReconnect { connectionId, reason, occurredAt }`
- `BankConnectionRemoved { connectionId, occurredAt }`
- `TransactionsIngested { connectionId, accountId, count, periodFrom, periodTo, occurredAt }`
- `AccountDiscovered { accountId, connectionId, accountKind, occurredAt }`

Estes eventos têm Event Mapper dedicado e a estrutura interna é totalmente desacoplada dos payloads da Pluggy.

### 6.5 Webhook handler — disciplina

1. Validar header customizado (segredo) — 401 se falhar.
2. Deduplicar por `eventId` (tabela `pluggy_events_seen` com TTL ou unique index).
3. Responder 200 em < 1s.
4. Enfileirar (channel + goroutine pool, ou fila externa) o processamento real.
5. No worker: invocar use cases do BC, que disparam Domain Events.

### 6.6 Cache do API Key

`apiKey` dura 2h. Não vale a pena bater `/auth/create` em toda chamada (gasta rate limit e latência). Cache em memória do adapter com refresh proativo aos ~110 minutos + retry on 401 (recria e tenta de novo uma vez). Sem rotação automática, o adapter trava quando o key expirar.

---

## 7. SDK Go oficial?

**Não existe.** A organização [github.com/pluggyai](https://github.com/pluggyai) mantém oficialmente apenas:

| SDK | Repo | Status |
|---|---|---|
| Node.js | https://github.com/pluggyai/pluggy-node | oficial, ativo |
| .NET | https://github.com/pluggyai/pluggy-net | oficial |
| Java | https://github.com/pluggyai/pluggy-java | oficial |
| JS client-side | https://github.com/pluggyai/pluggy-js | arquivado |
| Connect SDK (widget) | `pluggy-connect-sdk`, `react-pluggy-connect` no npm | oficial (frontend) |
| Python | https://github.com/diraol/pluggy-python | **comunidade**, não-oficial |
| **Go** | **não existe** | — |

A própria doc reconhece e diz: *"You can also do an integration yourself, by sending HTTP requests directly to our API"*, oferecendo o OAS em **https://api.pluggy.ai/oas3.json** para gerar clients.

### Implicação prática

O adapter Go vai ser HTTP cru. Duas opções:

1. **Manual** (recomendado para o PR4): `net/http` + structs DTO mínimas só dos campos que o BC consome. Mantém superfície pequena, fácil de testar com `httptest`, fácil de evoluir.
2. **Gerado**: rodar `oapi-codegen` ou `openapi-generator` contra o `oas3.json`. Gera muita coisa que não vai ser usada, mas tipa tudo. Bom se a expectativa é cobrir grande parte da API.

Para um BC que só precisa de: `auth`, `connect_token`, `items`, `accounts`, `v2/transactions`, `webhooks` — opção (1) é mais limpa, mais SOLID-friendly (interfaces pequenas, ISP), e o ACL fica explícito.

### Esqueleto de port sugerido (apenas para referência mental, não para copiar)

```go
// No domain layer (Evans-style: port pertence ao domínio)
type OpenFinanceProvider interface {
    InitiateConnection(ctx context.Context, userId UserId) (ConsentTicket, error)
    GetConnectionHealth(ctx context.Context, connectionId BankConnectionId) (Health, error)
    ListAccounts(ctx context.Context, connectionId BankConnectionId) ([]Account, error)
    ListTransactionsSince(ctx context.Context, accountId AccountId, since time.Time) (TransactionPage, error)
    Disconnect(ctx context.Context, connectionId BankConnectionId) error
}
```

O adapter Pluggy implementa essa interface — toda especificidade da Pluggy (IDs, status, paginação cursor, RSA encryption, retry de rate limit) fica encapsulada dentro.

Fonte: https://docs.pluggy.ai/docs/server-side-sdks e https://github.com/pluggyai

---

## Apêndice — Checklist de discovery antes do PR4

- [x] Conceitos centrais mapeados (Connector, Item, Account, Transaction, Bill, Investment, Identity)
- [x] Status do Item e executionStatus catalogados
- [x] Fluxo de consent compreendido (server gera connectToken → widget → itemId → backend persiste)
- [x] Endpoints principais identificados com seus rate limits
- [x] Webhooks: eventos, payloads e autenticação por header customizado
- [x] Pegadinhas de Transaction: sinal por tipo de conta, instabilidade do id, idempotência por fingerprint
- [x] Plano de ACL: renomeação de campos, Money, datas, mapping de erros
- [x] Domain Events do BC esboçados
- [x] Sem SDK Go oficial → adapter cru com `net/http`

### Pontos a verificar quando começar a implementar

- Formato exato do cursor em `/v2/transactions` (a doc é vaga; testar contra sandbox).
- Shape exato do response envelope (provavelmente `{ results: [...], next: "..." }`, confirmar no OAS).
- Se `eventId` é garantido único por evento (assumir que sim, mas confirmar com teste de duplicate delivery no sandbox).

---

## Fontes

- [Welcome to Pluggy](https://docs.pluggy.ai/)
- [Authentication](https://docs.pluggy.ai/docs/authentication)
- [Item](https://docs.pluggy.ai/docs/item)
- [Item Lifecycle](https://docs.pluggy.ai/docs/item-lifecycle)
- [Account](https://docs.pluggy.ai/docs/accounts)
- [Transaction](https://docs.pluggy.ai/docs/transactions)
- [Transactions List endpoint](https://docs.pluggy.ai/reference/transactions-list-1)
- [Credit Card Bills](https://docs.pluggy.ai/docs/credit-card-bills)
- [Investments](https://docs.pluggy.ai/docs/investments)
- [Identities](https://docs.pluggy.ai/docs/identities)
- [Connectors coverage](https://docs.pluggy.ai/docs/connectors-coverage)
- [Webhooks](https://docs.pluggy.ai/docs/webhooks)
- [Rate Limits](https://docs.pluggy.ai/docs/rate-limits)
- [Server-Side SDKs](https://docs.pluggy.ai/docs/server-side-sdks)
- [Pluggy on GitHub](https://github.com/pluggyai)
- [Glossary](https://docs.pluggy.ai/docs/glossary)
- [Postman: Pluggy Public](https://www.postman.com/pluggy-official/pluggy-public/documentation/wrl8bhb/pluggy)
- OpenAPI Spec: https://api.pluggy.ai/oas3.json

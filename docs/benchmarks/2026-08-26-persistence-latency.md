# Benchmark de persistência — Ent vs SQL cru

**Data:** 2026-08-26
**Motivação:** a persistência migrou de SQL escrito à mão (pgx) para Ent + Atlas em 2026-08-25. Esta medição responde quanto isso custou.

---

## 1. O que foi medido

Três camadas implementando as **mesmas portas do domínio**, com **SQL semanticamente idêntico** (upsert por id, insert multi-linha nas regras, duas queries na leitura com filho):

| Camada | Descrição |
|---|---|
| `pgxpool` | SQL à mão sobre a API nativa do pgx. É a reconstrução do antigo `adapter/pgxrepo`. |
| `sqldb` | SQL à mão sobre `database/sql` + pgx stdlib. |
| `ent` | Client gerado pelo Ent, que roda sobre `database/sql` + pgx stdlib. |

A camada do meio existe por rigor: o Ent **obrigatoriamente** atravessa `database/sql`, então comparar só `ent` contra `pgxpool` atribuiria ao ORM uma diferença que é em parte da camada `database/sql`. Com as três, o custo se separa.

## 2. Metodologia

- Postgres 17 em Docker no localhost, porta 55432, banco dedicado (`financial_manager_bench`), schema recriado das migrations versionadas.
- **Aquecimento** de 50 operações por camada antes de medir (primeira query prepara statement, primeira conexão faz handshake, Ent inicializa metadata).
- **Rodadas intercaladas**: 3 rodadas, cada uma executando todas as camadas em sequência, com as amostras agregadas. Isso dilui deriva do ambiente (throttle térmico, autovacuum, outro processo) que castigaria sempre quem rodasse por último.
- Cada operação é cronometrada individualmente; percentis por *nearest-rank*, sem interpolação — o valor reportado é uma medição real.
- Concorrência: 16 goroutines, 3000 operações, com acumulação local por goroutine (travar mutex por operação mediria a contenção do harness).
- Máquina: Apple M5, `darwin/arm64`. Duas execuções completas, para separar sinal de ruído.

Código: `internal/financialtracking/adapter/persistencebench/`, atrás da build tag `bench`. Rodar com `make bench-persistence`.

## 3. Latência sequencial (p50, duas execuções)

| Workload | pgxpool | sqldb | ent | Δ ent (exec. 1 / 2) |
|---|---|---|---|---|
| `account_insert` | 189µs / 168µs | 184µs / 166µs | 191µs / 170µs | +0,7% / +1,7% |
| `account_upsert` | 184µs / 164µs | 184µs / 165µs | 183µs / 171µs | −0,4% / +4,2% |
| `account_find` | 108µs / 96µs | 107µs / 97µs | 109µs / 102µs | +1,5% / +6,8% |
| `transaction_insert` | 198µs / 178µs | 188µs / 175µs | 202µs / 183µs | +2,4% / +3,1% |
| `transaction_find` | 110µs / 96µs | 108µs / 99µs | 114µs / 107µs | +3,4% / +10,7% |
| `category_save_5rules` | 671µs / 621µs | 662µs / 617µs | 694µs / 648µs | +3,3% / +4,3% |
| `category_find_5rules` | 217µs / 197µs | 221µs / 199µs | 230µs / 208µs | +5,6% / +5,9% |

Leitura concorrente (16 goroutines): p50 entre 483µs e 554µs nas três camadas, throughput entre 25k e 27,5k ops/s — **sem separação clara** entre elas.

## 4. Alocação por operação (`account_find`)

| Camada | ns/op | B/op | allocs/op |
|---|---|---|---|
| `pgxpool` | 110.197 | 2.021 | **41** |
| `sqldb` | 113.828 | 2.634 | **64** |
| `ent` | 116.392 | 7.967 | **185** |

## 5. Leitura dos números

**A latência não é onde o ORM cobra.** O Ent fica consistentemente acima (o *sinal* se repete nas duas execuções), mas entre +1% e +10% no p50, com magnitude variando mais que a diferença entre execuções da mesma camada. Em números absolutos: ~5 a 10µs por leitura. Numa operação de ~100µs dominada por round-trip, isso é irrelevante — e em produção, com rede real, a fração da camada Go fica **menor** ainda, não maior.

**A alocação é onde o ORM cobra: 4,5× mais alocações e 3,9× mais bytes por operação.** Isso não aparece na latência média porque a máquina está ociosa esperando o banco, mas escala com a taxa de requisições e reaparece como pressão de GC — ou seja, na **cauda**, no pior momento. É o número a observar se o serviço crescer, e a razão pra manter o benchmark no repositório em vez de rodá-lo uma vez e esquecer.

**`database/sql` custa quase nada** (`sqldb` ≈ `pgxpool` em latência, +56% em allocs). Então a diferença medida do Ent é dele mesmo, não da camada que ele atravessa — a variante do meio cumpriu o papel de não deixar o `database/sql` levar a culpa.

## 6. Conclusão prática

A troca se pagou: schema como código, migrations geradas e queries tipadas custaram ~5µs e ~144 alocações por operação. Para um gestor de gastos pessoal, é troca boa.

**Onde eu reconsideraria:** um endpoint de extrato paginando milhares de transações por request. Aí as 185 allocs/op se multiplicam por linha lida, e o caminho é usar `pgx` direto (com `CopyFrom`/batch) **só naquele caso de uso** — o que a arquitetura já permite, porque seria um segundo adapter implementando a mesma porta, sem tocar domínio nem use case.

**O que este benchmark NÃO mede:** custo de leituras em lote (as portas só têm `FindByID`), latência com rede real, comportamento com pool saturado, e o custo de manutenção das ~17k linhas geradas — que é real, mas não se mede em microssegundos.

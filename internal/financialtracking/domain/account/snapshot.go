package account

import (
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// AccountSnapshot é o PONTO DE MONTAGEM do aggregate para as bordas de
// persistência (padrão Memento, decisão de 2026-07-07). Existe por uma
// restrição real da linguagem: os campos do Account são unexported, então
// nenhum mapper fora deste package consegue remontar a conta lida do banco
// — e `Reconstitute` público na superfície do aggregate transformaria
// "rehidratar" numa operação de domínio, o que ela não é.
//
// Campos são PRIMITIVOS de propósito: é isso que o mapper do repository
// converte direto de/para colunas. VO nenhum atravessa esta fronteira.
//
// Não é DTO de domínio nem projeção de leitura: é a fotografia do estado
// interno, e só o repository tem motivo pra tocar nela.
type AccountSnapshot struct {
	ID     uuid.UUID
	UserID uuid.UUID
	Name   string
	Kind   string

	// Saldo desmontado em três colunas: quantia em centavos, moeda e o
	// instante a que o saldo se refere.
	BalanceAmount   int64
	BalanceCurrency string
	BalanceAsOf     time.Time

	// Origem: provider vazio = conta manual. O par (provider,
	// providerAccountID) é o que o VO Source exige junto — a invariante
	// "openfinance tem ref, manual não tem" é reconstruída no FromSnapshot.
	SourceProvider          string
	SourceProviderAccountID string
}

// Snapshot fotografa o estado atual da conta. Não emite evento e não muta
// nada: é caminho de SAÍDA (fromDomain no mapper).
func (a *Account) Snapshot() AccountSnapshot {
	money := a.balance.Money()
	currency := a.balance.Currency()
	provider, _ := a.source.Provider()
	providerAccountID, _ := a.source.ProviderAccountID()

	return AccountSnapshot{
		ID:                      a.id.UUID(),
		UserID:                  a.userID.UUID(),
		Name:                    a.name,
		Kind:                    a.kind.String(),
		BalanceAmount:           money.Amount(),
		BalanceCurrency:         currency.Code(),
		BalanceAsOf:             a.balance.AsOf(),
		SourceProvider:          provider,
		SourceProviderAccountID: providerAccountID,
	}
}

// FromSnapshot remonta a conta a partir da fotografia, revalidando TUDO
// pelos construtores dos VOs — banco corrompido não vira aggregate
// inválido. Não emite evento: rehidratar não é fato de domínio.
//
// Repare que a identidade vem do snapshot (AccountIDFromUUID), não é gerada:
// é a mesma conta, não uma nova.
func FromSnapshot(snapshot AccountSnapshot) (*Account, error) {
	id, err := AccountIDFromUUID(snapshot.ID)
	if err != nil {
		return nil, err
	}
	userID, err := shared.NewUserID(snapshot.UserID)
	if err != nil {
		return nil, err
	}
	canonicalName, err := normalizeName(snapshot.Name)
	if err != nil {
		return nil, err
	}
	kind, err := NewKind(snapshot.Kind)
	if err != nil {
		return nil, err
	}
	balance, err := balanceFromSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	source, err := sourceFromSnapshot(snapshot)
	if err != nil {
		return nil, err
	}

	return &Account{
		id:      id,
		userID:  userID,
		name:    canonicalName,
		kind:    kind,
		balance: balance,
		source:  source,
	}, nil
}

// balanceFromSnapshot remonta o VO Balance das três colunas de saldo.
func balanceFromSnapshot(snapshot AccountSnapshot) (Balance, error) {
	currency, err := shared.NewCurrency(snapshot.BalanceCurrency)
	if err != nil {
		return Balance{}, err
	}
	money, err := shared.NewMoney(snapshot.BalanceAmount, currency)
	if err != nil {
		return Balance{}, err
	}
	return NewBalance(money, snapshot.BalanceAsOf)
}

// sourceFromSnapshot escolhe o construtor certo pela presença do provider —
// é o polimorfismo da origem sem if espalhado: quem decide é a ausência do
// dado, e o construtor do VO valida o resto.
func sourceFromSnapshot(snapshot AccountSnapshot) (Source, error) {
	if snapshot.SourceProvider == "" {
		return NewManualSource(), nil
	}
	return NewOpenFinanceSource(snapshot.SourceProvider, snapshot.SourceProviderAccountID)
}

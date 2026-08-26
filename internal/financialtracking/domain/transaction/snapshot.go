package transaction

import (
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// TransactionSnapshot é o ponto de montagem do aggregate para as bordas de
// persistência (padrão Memento, decisão de 2026-07-07). Primitivos só —
// nenhum VO atravessa.
//
// Os dois blocos OPCIONAIS viram ponteiro/vazio: categoria (nil = não
// categorizada) e referência externa (provider vazio = lançamento manual).
// A alternativa — booleano "temCategoria" ao lado dos campos — deixaria dois
// jeitos de representar o mesmo estado, e um deles inevitavelmente mentiria.
type TransactionSnapshot struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	AccountID   uuid.UUID
	Amount      int64
	Currency    string
	OccurredAt  time.Time
	Description string
	Reconciled  bool

	// Atribuição de categoria: os três campos andam juntos ou nenhum vem.
	CategoryID         *uuid.UUID
	CategoryAssignedBy string
	CategoryAssignedAt time.Time

	// Referência no provider: provider vazio = transação manual.
	ExternalRefProvider              string
	ExternalRefProviderTransactionID string
}

// Snapshot fotografa o estado atual da transação. Não emite evento.
func (t *Transaction) Snapshot() TransactionSnapshot {
	snapshot := TransactionSnapshot{
		ID:          t.id.UUID(),
		UserID:      t.userID.UUID(),
		AccountID:   t.accountID.UUID(),
		Amount:      t.money.Amount(),
		Currency:    currencyCodeOf(t.money),
		OccurredAt:  t.occurredAt,
		Description: t.description,
		Reconciled:  t.reconciled,
	}

	if t.category != nil {
		assignment := *t.category
		categoryID := assignment.CategoryID()
		rawCategoryID := categoryID.UUID()
		assignedBy := assignment.By()

		snapshot.CategoryID = &rawCategoryID
		snapshot.CategoryAssignedBy = assignedBy.String()
		snapshot.CategoryAssignedAt = assignment.AssignedAt()
	}

	if t.externalRef != nil {
		ref := *t.externalRef
		snapshot.ExternalRefProvider = ref.Provider()
		snapshot.ExternalRefProviderTransactionID = ref.ProviderTransactionID()
	}

	return snapshot
}

// currencyCodeOf extrai o código da moeda da quantia — uma chamada por
// linha em vez de money.Currency().Code() no meio do literal.
func currencyCodeOf(money shared.Money) string {
	currency := money.Currency()
	return currency.Code()
}

// FromSnapshot remonta a transação revalidando tudo pelos construtores dos
// VOs. Não emite evento: rehidratar não é fato novo — em particular, uma
// transação importada NÃO re-emite Imported ao ser lida do banco.
func FromSnapshot(snapshot TransactionSnapshot) (*Transaction, error) {
	id, err := TransactionIDFromUUID(snapshot.ID)
	if err != nil {
		return nil, err
	}
	userID, err := shared.NewUserID(snapshot.UserID)
	if err != nil {
		return nil, err
	}
	accountID, err := account.AccountIDFromUUID(snapshot.AccountID)
	if err != nil {
		return nil, err
	}
	money, err := moneyFromSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	canonicalDescription, err := normalizeDescription(snapshot.Description)
	if err != nil {
		return nil, err
	}
	// Só o instante ZERO é recusado aqui — e NÃO a regra "não pode ser
	// futuro" do validateOccurredAt. Rehidratar não é criar: a invariante
	// de data futura vale no momento em que o fato é registrado; aplicá-la
	// na leitura tornaria uma transação gravada com relógio adiantado
	// ILEGÍVEL pra sempre, e o usuário perderia o lançamento por causa de
	// skew de clock.
	if snapshot.OccurredAt.IsZero() {
		return nil, ErrInvalidOccurredAt
	}
	assignment, err := assignmentFromSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	ref, err := refFromSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	if ref == nil && snapshot.Reconciled {
		// Estado impossível: só transação com contraparte no provider
		// concilia (a mesma regra do MarkReconciled).
		return nil, ErrNotReconcilable
	}

	return &Transaction{
		id:          id,
		userID:      userID,
		accountID:   accountID,
		money:       money,
		occurredAt:  snapshot.OccurredAt,
		description: canonicalDescription,
		category:    assignment,
		externalRef: ref,
		reconciled:  snapshot.Reconciled,
	}, nil
}

// moneyFromSnapshot remonta o VO Money das duas colunas de quantia.
func moneyFromSnapshot(snapshot TransactionSnapshot) (shared.Money, error) {
	currency, err := shared.NewCurrency(snapshot.Currency)
	if err != nil {
		return shared.Money{}, err
	}
	money, err := shared.NewMoney(snapshot.Amount, currency)
	if err != nil {
		return shared.Money{}, err
	}
	if money.IsZeroAmount() {
		return shared.Money{}, ErrZeroMoney
	}
	return money, nil
}

// assignmentFromSnapshot remonta a atribuição de categoria, ou nil quando a
// transação não está categorizada. Construtor próprio (não o
// NewCategoryAssignment) porque aqui o assignedAt vem do BANCO: usar o
// público datava a atribuição de agora e apagaria quando ela aconteceu.
func assignmentFromSnapshot(snapshot TransactionSnapshot) (*CategoryAssignment, error) {
	if snapshot.CategoryID == nil {
		return nil, nil
	}

	categoryID, err := category.CategoryIDFromUUID(*snapshot.CategoryID)
	if err != nil {
		return nil, err
	}
	assignedBy, err := NewAssignedBy(snapshot.CategoryAssignedBy)
	if err != nil {
		return nil, err
	}
	if snapshot.CategoryAssignedAt.IsZero() {
		return nil, ErrInvalidAssignment
	}

	assignment := CategoryAssignment{
		categoryID: categoryID,
		by:         assignedBy,
		assignedAt: snapshot.CategoryAssignedAt,
	}
	return &assignment, nil
}

// refFromSnapshot remonta a referência externa, ou nil se a transação é
// manual.
func refFromSnapshot(snapshot TransactionSnapshot) (*ExternalRef, error) {
	if snapshot.ExternalRefProvider == "" {
		return nil, nil
	}

	ref, err := NewExternalRef(snapshot.ExternalRefProvider, snapshot.ExternalRefProviderTransactionID)
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

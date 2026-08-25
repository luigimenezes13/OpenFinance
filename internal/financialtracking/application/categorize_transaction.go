package application

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/category"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

// CategorizeTransactionInput é a entrada da categorização. AssignedBy chega
// como TEXTO ("user" | "rule") — é VO de vocabulário, igual account.Kind:
// a palavra É a linguagem do domínio, então o construtor do VO valida.
type CategorizeTransactionInput struct {
	UserID        uuid.UUID
	TransactionID uuid.UUID
	CategoryID    uuid.UUID
	AssignedBy    string // "user" | "rule"
}

// CategorizeTransactionOutput é a projeção serializável da atribuição.
type CategorizeTransactionOutput struct {
	TransactionID string
	CategoryID    string
	AssignedBy    string
	AssignedAt    time.Time
}

// CategorizeTransactionUseCase atribui (ou troca) a categoria de uma
// transação. Três dependências: a transação (lê e grava), a categoria (só
// LÊ — pra garantir que existe e é do usuário) e o Dispatcher, esse sim
// necessário: Categorize emite Categorized, que o BC de Budgets consome no
// PR5.
type CategorizeTransactionUseCase struct {
	transactions transaction.Repository
	categories   category.Repository
	dispatcher   events.Dispatcher
}

// NewCategorizeTransactionUseCase injeta as dependências por construtor.
func NewCategorizeTransactionUseCase(transactions transaction.Repository, categories category.Repository, dispatcher events.Dispatcher) *CategorizeTransactionUseCase {
	return &CategorizeTransactionUseCase{
		transactions: transactions,
		categories:   categories,
		dispatcher:   dispatcher,
	}
}

// Execute orquestra a categorização: resolve identidades, carrega os dois
// aggregates pra checar dono, delega a mutação ao root e só então publica
// os eventos — a ordem Save → Dispatch → ClearEvents é a regra do spec §6
// (evento só sai depois do fato estar persistido).
func (u *CategorizeTransactionUseCase) Execute(ctx context.Context, input CategorizeTransactionInput) (CategorizeTransactionOutput, error) {
	userID, err := shared.NewUserID(input.UserID)
	if err != nil {
		return CategorizeTransactionOutput{}, err
	}

	transactionID, err := transaction.TransactionIDFromUUID(input.TransactionID)
	if err != nil {
		return CategorizeTransactionOutput{}, err
	}

	categoryID, err := category.CategoryIDFromUUID(input.CategoryID)
	if err != nil {
		return CategorizeTransactionOutput{}, err
	}

	assignedBy, err := transaction.NewAssignedBy(input.AssignedBy)
	if err != nil {
		return CategorizeTransactionOutput{}, err
	}

	target, err := u.transactions.FindByID(ctx, transactionID)
	if err != nil {
		return CategorizeTransactionOutput{}, err
	}
	if err := assertOwnership(target.UserID(), userID); err != nil {
		return CategorizeTransactionOutput{}, err
	}

	// A categoria é carregada só pra VALIDAR: existe? é deste usuário?
	// Categorizar com o id de uma categoria de outra pessoa vazaria a
	// existência dela — daí o ErrForbidden vir antes de qualquer mutação.
	targetCategory, err := u.categories.FindByID(ctx, categoryID)
	if err != nil {
		return CategorizeTransactionOutput{}, err
	}
	if err := assertOwnership(targetCategory.UserID(), userID); err != nil {
		return CategorizeTransactionOutput{}, err
	}

	// Recategorizar SUBSTITUI a atribuição e re-emite o evento — decisão de
	// domínio já resolvida dentro do aggregate (2026-07-07).
	if err := target.Categorize(categoryID, assignedBy); err != nil {
		return CategorizeTransactionOutput{}, err
	}

	if err := u.transactions.Save(ctx, target); err != nil {
		return CategorizeTransactionOutput{}, err
	}

	if err := dispatchAndClear(ctx, u.dispatcher, target); err != nil {
		return CategorizeTransactionOutput{}, err
	}

	return toCategorizeTransactionOutput(target)
}

// recorder é o contrato mínimo que um aggregate com eventos satisfaz —
// exatamente os três métodos que o kernel.EventRecorder promove. Declarar a
// interface AQUI (no consumidor, do tamanho do uso) é o idioma Go: quem
// define a abstração é quem depende dela, não quem a implementa.
type recorder interface {
	Events() []events.Event
	ClearEvents()
}

// dispatchAndClear publica os eventos acumulados e limpa o histórico.
// Concentra num lugar a ordem exigida pelo spec §6 e o cuidado de não
// limpar o histórico se o dispatch falhar — no v1 o dispatcher é síncrono,
// então erro de handler aborta o use case e os eventos ficam no aggregate.
func dispatchAndClear(ctx context.Context, dispatcher events.Dispatcher, aggregate recorder) error {
	recorded := aggregate.Events()
	if len(recorded) == 0 {
		return nil
	}
	if err := dispatcher.Dispatch(ctx, recorded...); err != nil {
		return err
	}
	aggregate.ClearEvents()
	return nil
}

// toCategorizeTransactionOutput projeta a atribuição recém-feita. Retorna
// erro junto porque a leitura é comma-ok: se por absurdo a transação não
// estiver categorizada aqui, é bug de invariante — melhor um erro explícito
// que um output com campos zerados.
func toCategorizeTransactionOutput(target *transaction.Transaction) (CategorizeTransactionOutput, error) {
	assignment, ok := target.Category()
	if !ok {
		return CategorizeTransactionOutput{}, transaction.ErrInvalidAssignment
	}

	assignedCategory := assignment.CategoryID()
	assignedBy := assignment.By()

	return CategorizeTransactionOutput{
		TransactionID: target.ID().String(),
		CategoryID:    assignedCategory.String(),
		AssignedBy:    assignedBy.String(),
		AssignedAt:    assignment.AssignedAt(),
	}, nil
}

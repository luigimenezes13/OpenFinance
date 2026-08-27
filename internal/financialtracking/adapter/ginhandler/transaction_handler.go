package ginhandler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
)

// TransactionHandler expõe as operações de transação. Três use cases, três
// métodos — o handler não decide qual regra roda, só qual caso de uso.
type TransactionHandler struct {
	recordTransaction    *application.RecordTransactionUseCase
	categorizeTransation *application.CategorizeTransactionUseCase
	importFromProvider   *application.ImportFromProviderUseCase
	logger               *slog.Logger
}

// NewTransactionHandler injeta as dependências por construtor.
func NewTransactionHandler(
	recordTransaction *application.RecordTransactionUseCase,
	categorizeTransaction *application.CategorizeTransactionUseCase,
	importFromProvider *application.ImportFromProviderUseCase,
	logger *slog.Logger,
) *TransactionHandler {
	return &TransactionHandler{
		recordTransaction:    recordTransaction,
		categorizeTransation: categorizeTransaction,
		importFromProvider:   importFromProvider,
		logger:               logger,
	}
}

// recordTransactionRequest é o corpo do lançamento manual.
//
// Amount NÃO tem binding:"required" de propósito: em Go, `required` recusa o
// zero value, então valor 0 viraria 400 "campo ausente" em vez do 422
// zero_amount que o domínio produz. A regra é do aggregate; o binding só
// cuida de sintaxe.
//
// Não há campo de moeda: a transação herda a moeda da conta (decisão de
// 2026-08-25).
type recordTransactionRequest struct {
	AccountID   string    `json:"account_id" binding:"required"`
	Amount      int64     `json:"amount"`
	OccurredAt  time.Time `json:"occurred_at" binding:"required"`
	Description string    `json:"description" binding:"required"`
}

// Record atende POST /v1/transactions.
func (h *TransactionHandler) Record(context *gin.Context) {
	var request recordTransactionRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		respondBadRequest(context, err.Error())
		return
	}

	accountID, err := uuid.Parse(request.AccountID)
	if err != nil {
		respondBadRequest(context, "account_id não é um uuid válido")
		return
	}

	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	output, err := h.recordTransaction.Execute(context.Request.Context(), application.RecordTransactionInput{
		UserID:      userID,
		AccountID:   accountID,
		Amount:      request.Amount,
		OccurredAt:  request.OccurredAt,
		Description: request.Description,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	context.JSON(http.StatusCreated, toTransactionResponse(output))
}

// categorizeTransactionRequest é o corpo da categorização. assigned_by é
// opcional: o default "user" cobre o caso do humano corrigindo a categoria,
// que é a origem de 100% das chamadas por HTTP hoje (regra automática não
// passa pela API).
type categorizeTransactionRequest struct {
	CategoryID string `json:"category_id" binding:"required"`
	AssignedBy string `json:"assigned_by"`
}

// defaultAssignedBy é a origem assumida quando o corpo não diz.
const defaultAssignedBy = "user"

// Categorize atende PUT /v1/transactions/:id/category.
func (h *TransactionHandler) Categorize(context *gin.Context) {
	transactionID, err := uuid.Parse(context.Param("id"))
	if err != nil {
		respondBadRequest(context, "id da transação não é um uuid válido")
		return
	}

	var request categorizeTransactionRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		respondBadRequest(context, err.Error())
		return
	}

	categoryID, err := uuid.Parse(request.CategoryID)
	if err != nil {
		respondBadRequest(context, "category_id não é um uuid válido")
		return
	}

	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	assignedBy := request.AssignedBy
	if assignedBy == "" {
		assignedBy = defaultAssignedBy
	}

	output, err := h.categorizeTransation.Execute(context.Request.Context(), application.CategorizeTransactionInput{
		UserID:        userID,
		TransactionID: transactionID,
		CategoryID:    categoryID,
		AssignedBy:    assignedBy,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	// 200, não 201: categorizar MUTA uma transação existente, não cria
	// recurso novo.
	context.JSON(http.StatusOK, toCategorizationResponse(output))
}

// importFromProviderRequest é o corpo da importação. `since` é opcional
// (ausente = todo o histórico disponível no provider).
//
// Só a conta LOCAL entra: a referência do provider sai do VO Source dela.
// Aceitar id de provider no corpo deixaria qualquer um importar pra conta de
// qualquer um.
type importFromProviderRequest struct {
	AccountID string     `json:"account_id" binding:"required"`
	Since     *time.Time `json:"since"`
}

// Import atende POST /v1/transactions/import.
func (h *TransactionHandler) Import(context *gin.Context) {
	var request importFromProviderRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		respondBadRequest(context, err.Error())
		return
	}

	accountID, err := uuid.Parse(request.AccountID)
	if err != nil {
		respondBadRequest(context, "account_id não é um uuid válido")
		return
	}

	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	var since time.Time
	if request.Since != nil {
		since = *request.Since
	}

	output, err := h.importFromProvider.Execute(context.Request.Context(), application.ImportFromProviderInput{
		UserID:    userID,
		AccountID: accountID,
		Since:     since,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	context.JSON(http.StatusCreated, toImportResponse(output))
}

// transactionResponse é o DTO de saída do lançamento.
type transactionResponse struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	AccountID   string    `json:"account_id"`
	Amount      int64     `json:"amount"`
	Currency    string    `json:"currency"`
	OccurredAt  time.Time `json:"occurred_at"`
	Description string    `json:"description"`
}

// toTransactionResponse traduz o Output do use case.
func toTransactionResponse(output application.RecordTransactionOutput) transactionResponse {
	return transactionResponse{
		ID:          output.ID,
		UserID:      output.UserID,
		AccountID:   output.AccountID,
		Amount:      output.Amount,
		Currency:    output.Currency,
		OccurredAt:  output.OccurredAt,
		Description: output.Description,
	}
}

// categorizationResponse é o DTO de saída da categorização.
type categorizationResponse struct {
	TransactionID string    `json:"transaction_id"`
	CategoryID    string    `json:"category_id"`
	AssignedBy    string    `json:"assigned_by"`
	AssignedAt    time.Time `json:"assigned_at"`
}

// toCategorizationResponse traduz o Output do use case.
func toCategorizationResponse(output application.CategorizeTransactionOutput) categorizationResponse {
	return categorizationResponse{
		TransactionID: output.TransactionID,
		CategoryID:    output.CategoryID,
		AssignedBy:    output.AssignedBy,
		AssignedAt:    output.AssignedAt,
	}
}

// importResponse é o DTO de saída da importação. imported_count vem
// explícito em vez de o cliente contar o array: é o número que interessa
// pra UI, e mandá-lo pronto evita que dois clientes contem de formas
// diferentes.
type importResponse struct {
	AccountID      string   `json:"account_id"`
	Provider       string   `json:"provider"`
	TransactionIDs []string `json:"transaction_ids"`
	ImportedCount  int      `json:"imported_count"`

	// O saldo vai na resposta porque a importação é o ÚNICO caminho pelo
	// qual ele muda: quem chamou acabou de causar a mudança e precisa do
	// número novo sem uma segunda requisição.
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`

	// false = o provedor devolveu saldo mais antigo que o registrado e ele
	// foi ignorado. Sai explícito pra a UI não anunciar "atualizado agora"
	// quando nada mudou.
	BalanceApplied bool `json:"balance_applied"`
}

// toImportResponse traduz o Output do use case.
func toImportResponse(output application.ImportFromProviderOutput) importResponse {
	return importResponse{
		AccountID:      output.AccountID,
		Provider:       output.Provider,
		TransactionIDs: output.TransactionIDs,
		ImportedCount:  len(output.TransactionIDs),
		Balance:        output.Balance,
		Currency:       output.Currency,
		BalanceApplied: output.BalanceApplied,
	}
}

package ginhandler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
)

// AccountHandler expõe as operações de conta.
type AccountHandler struct {
	createAccount *application.CreateAccountUseCase
	listAccounts  *application.ListAccountsUseCase
	viewAccount   *application.ViewAccountUseCase
	renameAccount *application.RenameAccountUseCase
	logger        *slog.Logger
}

// NewAccountHandler injeta as dependências por construtor.
func NewAccountHandler(
	createAccount *application.CreateAccountUseCase,
	listAccounts *application.ListAccountsUseCase,
	viewAccount *application.ViewAccountUseCase,
	renameAccount *application.RenameAccountUseCase,
	logger *slog.Logger,
) *AccountHandler {
	return &AccountHandler{
		createAccount: createAccount,
		listAccounts:  listAccounts,
		viewAccount:   viewAccount,
		renameAccount: renameAccount,
		logger:        logger,
	}
}

// createAccountRequest é o corpo aceito. DTO LOCAL do handler: as tags de
// binding e o vocabulário HTTP (snake_case) não contaminam o Input do use
// case.
//
// Repare que o dono da conta NÃO vem no corpo — vem do header, pelo
// middleware. Aceitar user_id no payload deixaria qualquer um criar conta
// no nome de outro.
type createAccountRequest struct {
	Name     string `json:"name" binding:"required"`
	Kind     string `json:"kind" binding:"required"`
	Currency string `json:"currency" binding:"required"`
}

// Create atende POST /v1/accounts.
func (h *AccountHandler) Create(context *gin.Context) {
	var request createAccountRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		respondBadRequest(context, err.Error())
		return
	}

	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	output, err := h.createAccount.Execute(context.Request.Context(), application.CreateAccountInput{
		UserID:   userID,
		Name:     request.Name,
		Kind:     request.Kind,
		Currency: request.Currency,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	context.JSON(http.StatusCreated, toAccountResponse(output))
}

// accountResponse é o DTO de SAÍDA da borda. Existe pelo mesmo motivo do
// request DTO: o vocabulário HTTP (snake_case) é da borda, não da
// application. Sem ele, o JSON exporia os nomes de campo Go do Output
// ("ID", "UserID"), e renomear um campo interno viraria breaking change de
// API sem ninguém perceber.
type accountResponse struct {
	ID       string `json:"id"`
	UserID   string `json:"user_id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`
}

// toAccountResponse traduz o Output do use case no corpo da resposta.
func toAccountResponse(output application.CreateAccountOutput) accountResponse {
	return accountResponse{
		ID:       output.ID,
		UserID:   output.UserID,
		Name:     output.Name,
		Kind:     output.Kind,
		Balance:  output.Balance,
		Currency: output.Currency,
	}
}

// List atende GET /v1/accounts.
func (h *AccountHandler) List(context *gin.Context) {
	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	output, err := h.listAccounts.Execute(context.Request.Context(), application.ListAccountsInput{
		UserID: userID,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	summaries := make([]accountSummaryResponse, 0, len(output.Accounts))
	for _, current := range output.Accounts {
		summaries = append(summaries, toAccountSummaryResponse(current))
	}

	// Envelope com `accounts` em vez de array na raiz: array na raiz não tem
	// para onde crescer, e a primeira necessidade de metadado (total,
	// paginação) viraria breaking change.
	context.JSON(http.StatusOK, gin.H{"accounts": summaries})
}

// Get atende GET /v1/accounts/:id.
func (h *AccountHandler) Get(context *gin.Context) {
	accountID, err := uuid.Parse(context.Param("id"))
	if err != nil {
		respondBadRequest(context, "id da conta não é um uuid válido")
		return
	}

	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	summary, err := h.viewAccount.Execute(context.Request.Context(), application.ViewAccountInput{
		UserID:    userID,
		AccountID: accountID,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	context.JSON(http.StatusOK, toAccountSummaryResponse(summary))
}

// accountSummaryResponse é o DTO de leitura de conta.
type accountSummaryResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Balance     int64     `json:"balance"`
	Currency    string    `json:"currency"`
	BalanceAsOf time.Time `json:"balance_as_of"`

	// null = conta manual. O cliente usa isso pra saber se o saldo é
	// confiável (veio do banco) ou se a conta é só registro manual.
	Provider *string `json:"provider"`
}

// toAccountSummaryResponse traduz a projeção do use case.
func toAccountSummaryResponse(summary application.AccountSummary) accountSummaryResponse {
	return accountSummaryResponse{
		ID:          summary.ID,
		Name:        summary.Name,
		Kind:        summary.Kind,
		Balance:     summary.Balance,
		Currency:    summary.Currency,
		BalanceAsOf: summary.BalanceAsOf,
		Provider:    optionalString(summary.Provider),
	}
}

// optionalString devolve nil para string vazia, pra o JSON sair com `null`.
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// renameAccountRequest é o corpo da renomeação. Só `name`: kind e moeda são
// decisões de abertura da conta (trocar a moeda reinterpretaria todo o
// histórico de lançamentos), e saldo vem do provedor.
type renameAccountRequest struct {
	Name string `json:"name" binding:"required"`
}

// Rename atende PATCH /v1/accounts/:id.
//
// PATCH e não PUT: o corpo carrega UM campo, não o recurso inteiro. PUT
// prometeria substituição total, e um cliente que mandasse só `name`
// esperaria — corretamente — que o resto fosse apagado.
func (h *AccountHandler) Rename(context *gin.Context) {
	accountID, err := uuid.Parse(context.Param("id"))
	if err != nil {
		respondBadRequest(context, "id da conta não é um uuid válido")
		return
	}

	var request renameAccountRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		respondBadRequest(context, err.Error())
		return
	}

	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	summary, err := h.renameAccount.Execute(context.Request.Context(), application.RenameAccountInput{
		UserID:    userID,
		AccountID: accountID,
		Name:      request.Name,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	context.JSON(http.StatusOK, toAccountSummaryResponse(summary))
}

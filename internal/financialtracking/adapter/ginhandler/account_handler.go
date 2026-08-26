package ginhandler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
)

// AccountHandler expõe as operações de conta.
type AccountHandler struct {
	createAccount *application.CreateAccountUseCase
	logger        *slog.Logger
}

// NewAccountHandler injeta as dependências por construtor.
func NewAccountHandler(createAccount *application.CreateAccountUseCase, logger *slog.Logger) *AccountHandler {
	return &AccountHandler{createAccount: createAccount, logger: logger}
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

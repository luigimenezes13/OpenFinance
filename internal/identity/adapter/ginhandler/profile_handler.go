// Package ginhandler é a borda HTTP de saída do bounded context Identity.
//
// Mesmas regras do ginhandler do Financial Tracking: handler burro (lê a
// identidade, chama UM use case, responde), DTO de resposta local em
// snake_case, e tradução de erro centralizada em platform/httperror.
package ginhandler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/ginmiddleware"
	"github.com/luigimenezes13/financial-manager/internal/identity/application"
	"github.com/luigimenezes13/financial-manager/internal/platform/httperror"
)

// ProfileHandler expõe o perfil do usuário autenticado.
type ProfileHandler struct {
	viewProfile *application.ViewProfileUseCase
	logger      *slog.Logger
}

// NewProfileHandler injeta as dependências por construtor.
func NewProfileHandler(viewProfile *application.ViewProfileUseCase, logger *slog.Logger) *ProfileHandler {
	return &ProfileHandler{viewProfile: viewProfile, logger: logger}
}

// profileResponse é o DTO de saída da borda.
type profileResponse struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`

	// Ponteiro pra sair como `null` quando não há avatar, em vez de string
	// vazia: `""` obrigaria o cliente a tratar "sem foto" como um caso
	// especial de string, e `null` já diz isso na própria forma.
	AvatarURL *string `json:"avatar_url"`

	RegisteredAt time.Time `json:"registered_at"`
}

// optionalString devolve nil para string vazia, pra o JSON sair com `null`.
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// Me atende GET /v1/me.
//
// Não há rota equivalente com id no caminho (GET /v1/users/:id): quem
// responde é sempre o portador do token. Sem o parâmetro, não existe a
// pergunta "posso ver o perfil de outro?" — e portanto não existe a falha de
// autorização correspondente.
func (h *ProfileHandler) Me(context *gin.Context) {
	userID, ok := ginmiddleware.UserIDFrom(context.Request.Context())
	if !ok {
		// Ausência aqui é bug de fiação (rota registrada fora do grupo
		// autenticado), não erro de cliente — por isso 500 e não 401.
		h.logger.ErrorContext(context.Request.Context(), "rota sem middleware de identidade",
			slog.String("path", context.FullPath()))
		context.JSON(http.StatusInternalServerError, httperror.Body{
			Error: httperror.Detail{Code: "internal_error", Message: "erro interno"},
		})
		return
	}

	output, err := h.viewProfile.Execute(context.Request.Context(), application.ViewProfileInput{
		UserID: userID,
	})
	if err != nil {
		status, body := httperror.Translate(err)
		if status >= http.StatusInternalServerError {
			h.logger.ErrorContext(context.Request.Context(), "erro não mapeado na borda HTTP",
				slog.String("error", err.Error()), slog.String("path", context.FullPath()))
		}
		context.JSON(status, body)
		return
	}

	context.JSON(http.StatusOK, profileResponse{
		UserID:       output.UserID,
		Email:        output.Email,
		Name:         output.Name,
		AvatarURL:    optionalString(output.AvatarURL),
		RegisteredAt: output.RegisteredAt,
	})
}

// Package ginhandler é a borda HTTP de ENTRADA do Financial Tracking BC.
//
// Handlers são BURROS, por regra: (1) fazem bind do corpo, (2) leem a
// identidade do context, (3) chamam UM use case, (4) respondem. Zero regra
// de negócio, zero decisão de status inventada localmente — a tradução de
// erro de domínio pra HTTP mora em platform/httperror.
//
// O que atravessa a fronteira: request DTO local (com tags de binding) →
// Input do use case; Output do use case → JSON. O aggregate nunca vaza pra
// cá, e gin nunca desce pro use case (que recebe context.Context puro).
package ginhandler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/ginmiddleware"
	"github.com/luigimenezes13/financial-manager/internal/platform/httperror"
)

// respondError traduz erro de domínio em status e corpo. Um lugar só: se
// cada handler decidisse, o mesmo ErrForbidden viraria 403 aqui e 404 lá.
//
// O 500 é logado com o erro COMPLETO; o cliente recebe a mensagem genérica
// que o httperror monta. Detalhe de infraestrutura (host do banco, nome de
// coluna) fica no log, não na resposta.
func respondError(context *gin.Context, logger *slog.Logger, err error) {
	status, body := httperror.Translate(err)

	if status >= http.StatusInternalServerError {
		logger.ErrorContext(context.Request.Context(), "erro não mapeado na borda HTTP",
			slog.String("error", err.Error()),
			slog.String("path", context.FullPath()),
		)
	}

	context.JSON(status, body)
}

// respondBadRequest responde falha de SINTAXE (JSON malformado, campo
// obrigatório ausente). Separado do respondError de propósito: 400 é "não
// entendi a requisição", 422 é "entendi e o domínio recusou".
func respondBadRequest(context *gin.Context, message string) {
	context.JSON(http.StatusBadRequest, httperror.BadRequest(message))
}

// userIDFrom lê a identidade injetada pelo middleware. Ausência aqui é bug
// de wiring (rota registrada fora do grupo autenticado), não erro de
// cliente — por isso 500 e não 401.
func userIDFrom(context *gin.Context) (uuid.UUID, bool) {
	userID, ok := ginmiddleware.UserIDFrom(context.Request.Context())
	if !ok {
		context.JSON(http.StatusInternalServerError,
			httperror.Body{Error: httperror.Detail{Code: "internal_error", Message: "erro interno"}})
		return uuid.Nil, false
	}
	return userID, true
}

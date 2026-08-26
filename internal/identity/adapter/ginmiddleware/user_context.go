// Package ginmiddleware é a borda de ENTRADA do bounded context Identity:
// resolve quem está fazendo a requisição e injeta a identidade no
// context.Context que atravessa as camadas.
//
// ESTADO DO V1, explícito: o middleware CONFIA no header X-User-Id. Não há
// autenticação — nem token, nem sessão, nem o aggregate User do spec §3.
// Isso é aceitável só em desenvolvimento, e está aqui por dois motivos: o
// contrato pra dentro (use case recebe uuid já parseado no context) é o
// mesmo que valeria com JWT, e a troca de "confia no header" por "valida
// token" não toca handler nem use case — só este arquivo.
package ginmiddleware

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/platform/httperror"
)

// HeaderUserID é o header que carrega a identidade no v1.
const HeaderUserID = "X-User-Id"

// contextKey é um tipo PRIVADO usado como chave de context. String solta
// como chave é colisão esperando acontecer: qualquer package poderia
// escrever na mesma chave sem saber. Com tipo privado, só este package
// consegue produzir a chave.
type contextKey struct{}

// UserContext extrai e valida o X-User-Id, injeta no context da request e
// segue. Header ausente ou mal formado morre aqui com 401 — é a fronteira
// de tradução: string → uuid acontece na borda, nunca no domínio.
func UserContext() gin.HandlerFunc {
	return func(context *gin.Context) {
		raw := context.GetHeader(HeaderUserID)
		if raw == "" {
			context.AbortWithStatusJSON(http.StatusUnauthorized,
				httperror.BadRequest("header "+HeaderUserID+" é obrigatório"))
			return
		}

		userID, err := uuid.Parse(raw)
		if err != nil {
			context.AbortWithStatusJSON(http.StatusUnauthorized,
				httperror.BadRequest("header "+HeaderUserID+" não é um uuid válido"))
			return
		}

		// O uuid vai no context da REQUEST (não só no gin.Context) porque é
		// o context.Context que desce pros use cases — eles não conhecem gin.
		context.Request = context.Request.WithContext(withUserID(context.Request.Context(), userID))
		context.Next()
	}
}

// withUserID guarda a identidade no context.
func withUserID(parent context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(parent, contextKey{}, userID)
}

// UserIDFrom lê a identidade do context. Comma-ok: ausência é possível
// (rota sem o middleware), e o chamador precisa decidir o que fazer.
func UserIDFrom(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(contextKey{}).(uuid.UUID)
	return userID, ok
}

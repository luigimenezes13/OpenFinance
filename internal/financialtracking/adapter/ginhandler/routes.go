package ginhandler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/ginmiddleware"
)

// RegisterRoutes monta as rotas do BC. Um lugar só declara o desenho da
// API: quem procura "onde está o endpoint X" tem UM arquivo pra abrir.
//
// O middleware de identidade é aplicado ao GRUPO /v1, não rota por rota —
// esquecer o middleware numa rota nova seria um furo de autorização
// silencioso, e agrupar torna isso impossível por construção.
func RegisterRoutes(
	router *gin.Engine,
	database *sql.DB,
	accounts *AccountHandler,
	transactions *TransactionHandler,
	categories *CategoryHandler,
) {
	// Health check FORA do grupo autenticado: orquestrador (k8s, compose)
	// não manda header de usuário.
	router.GET("/healthz", healthz(database))

	authenticated := router.Group("/v1", ginmiddleware.UserContext())

	authenticated.POST("/accounts", accounts.Create)
	authenticated.POST("/categories", categories.Create)
	authenticated.POST("/transactions", transactions.Record)
	authenticated.PUT("/transactions/:id/category", transactions.Categorize)
	authenticated.POST("/transactions/import", transactions.Import)

	// Endpoints de LEITURA (GET) ficaram fora do v1: não existe use case de
	// consulta, e inventar um handler que fala direto com o repositório
	// furaria a regra de "toda ação é um use case". Entram junto com os
	// casos de uso de consulta.
}

// healthz responde saúde do processo COM checagem de dependência: um
// /healthz que só devolve 200 mente quando o banco cai — o orquestrador
// mantém no ar um processo que não atende ninguém.
func healthz(database *sql.DB) gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		ctx, cancel := context.WithTimeout(ginContext.Request.Context(), healthTimeout)
		defer cancel()

		if err := database.PingContext(ctx); err != nil {
			ginContext.JSON(http.StatusServiceUnavailable, gin.H{
				"status":   "unhealthy",
				"database": "unreachable",
			})
			return
		}

		ginContext.JSON(http.StatusOK, gin.H{"status": "healthy", "database": "reachable"})
	}
}

// healthTimeout limita o Ping: health check que pendura é pior que health
// check que falha — o orquestrador fica esperando em vez de agir.
const healthTimeout = 2 * time.Second

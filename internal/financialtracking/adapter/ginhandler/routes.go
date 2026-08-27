package ginhandler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes monta as rotas do BC. Um lugar só declara o desenho da
// API: quem procura "onde está o endpoint X" tem UM arquivo pra abrir.
//
// O middleware de identidade é aplicado ao GRUPO /v1, não rota por rota —
// esquecer o middleware numa rota nova seria um furo de autorização
// silencioso, e agrupar torna isso impossível por construção.
func RegisterRoutes(
	router *gin.Engine,
	database Pinger,
	authenticate gin.HandlerFunc,
	accounts *AccountHandler,
	transactions *TransactionHandler,
	categories *CategoryHandler,
) {
	// Health check FORA do grupo autenticado: orquestrador (k8s, compose)
	// não manda header de usuário.
	router.GET("/healthz", healthz(database))

	// O middleware de identidade chega PRONTO por parâmetro: este package é
	// o Financial Tracking, e ele não tem por que conhecer o bounded context
	// Identity nem como uma credencial é verificada. Ele exige apenas que
	// alguém tenha resolvido o usuário antes.
	authenticated := router.Group("/v1", authenticate)

	authenticated.POST("/accounts", accounts.Create)
	authenticated.GET("/accounts", accounts.List)
	authenticated.GET("/accounts/:id", accounts.Get)
	authenticated.PATCH("/accounts/:id", accounts.Rename)

	authenticated.POST("/categories", categories.Create)
	authenticated.GET("/categories", categories.List)
	authenticated.PATCH("/categories/:id", categories.Rename)
	authenticated.PUT("/categories/:id/parent", categories.Move)
	authenticated.POST("/categories/:id/rules", categories.AddRule)
	authenticated.DELETE("/categories/:id/rules/:ruleId", categories.RemoveRule)

	authenticated.POST("/transactions", transactions.Record)
	authenticated.GET("/transactions", transactions.List)
	authenticated.GET("/transactions/:id", transactions.Get)
	authenticated.PUT("/transactions/:id/category", transactions.Categorize)
	authenticated.POST("/transactions/import", transactions.Import)
}

// Pinger é o que a rota de saúde precisa do banco: UMA operação.
//
// A rota recebia *sql.DB concreto, o que a tornava impossível de testar sem
// um Postgres de verdade — e um health check é justamente o que precisa ter
// teste do caminho de FALHA. A interface é declarada aqui, no consumidor, do
// tamanho exato do uso (ISP): *sql.DB a satisfaz sem saber que ela existe.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// healthz responde saúde do processo COM checagem de dependência: um
// /healthz que só devolve 200 mente quando o banco cai — o orquestrador
// mantém no ar um processo que não atende ninguém.
func healthz(database Pinger) gin.HandlerFunc {
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

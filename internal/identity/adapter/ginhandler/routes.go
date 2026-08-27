package ginhandler

import "github.com/gin-gonic/gin"

// RegisterRoutes monta as rotas deste bounded context.
//
// Cada BC registra as SUAS rotas, recebendo o middleware de autenticação
// pronto — o mesmo desenho do Financial Tracking. Assim nenhum dos dois
// precisa conhecer as rotas do outro, e o composition root é o único lugar
// que vê a API completa.
func RegisterRoutes(router *gin.Engine, authenticate gin.HandlerFunc, profile *ProfileHandler) {
	authenticated := router.Group("/v1", authenticate)

	authenticated.GET("/me", profile.Me)
}

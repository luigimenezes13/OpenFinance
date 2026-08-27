// Package httpmiddleware reúne middlewares HTTP cross-cutting: não pertencem
// a bounded context nenhum, por isso vivem em platform/.
package httpmiddleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Cabeçalhos e valores do CORS.
const (
	headerOrigin    = "Origin"
	headerVary      = "Vary"
	allowedMethods  = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	allowedHeaders  = "Authorization, Content-Type"
	preflightMaxAge = "600"
)

// CORS libera o navegador a chamar esta API a partir das origens permitidas.
//
// Existe porque o Sign-In do Google acontece no frontend: sem estes
// cabeçalhos o browser bloqueia a requisição do SPA e o backend nem fica
// sabendo — o erro aparece só no console do usuário.
//
// A lista de origens é EXPLÍCITA, nunca `*`. Mesmo sem cookies (usamos Bearer
// token, então `*` seria tecnicamente aceito pelo browser), `*` significa
// "qualquer site pode disparar chamadas autenticadas se conseguir um token" —
// e uma allowlist custa uma variável de ambiente.
//
// Lista vazia = nenhum cabeçalho CORS. O default é o comportamento mais
// restrito: serviço sem configuração não deve ficar aberto por omissão.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}

	return func(ginContext *gin.Context) {
		origin := ginContext.GetHeader(headerOrigin)

		// Vary: Origin SEMPRE que a resposta pode variar por origem, mesmo
		// quando a origem é recusada. Sem ele, um cache intermediário entrega
		// a resposta de uma origem permitida para outra que não é.
		ginContext.Header(headerVary, headerOrigin)

		if _, ok := allowed[origin]; !ok {
			// Origem desconhecida, ou requisição sem Origin (curl, servidor a
			// servidor): segue sem cabeçalho CORS. Recusar aqui quebraria
			// clientes que não são navegador.
			ginContext.Next()
			return
		}

		ginContext.Header("Access-Control-Allow-Origin", origin)
		ginContext.Header("Access-Control-Allow-Methods", allowedMethods)
		ginContext.Header("Access-Control-Allow-Headers", allowedHeaders)
		ginContext.Header("Access-Control-Max-Age", preflightMaxAge)

		// Preflight termina AQUI, com 204 e sem corpo: deixar o OPTIONS
		// seguir cairia no middleware de autenticação, que responderia 401 —
		// e o browser interpretaria como "CORS negado", escondendo a causa.
		if ginContext.Request.Method == http.MethodOptions {
			ginContext.AbortWithStatus(http.StatusNoContent)
			return
		}

		ginContext.Next()
	}
}

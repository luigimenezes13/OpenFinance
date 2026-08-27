package httpmiddleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/platform/httpmiddleware"
)

func TestMain(main *testing.M) {
	gin.SetMode(gin.TestMode)
	main.Run()
}

// newRouter monta uma rota com o middleware e um handler que sempre responde
// 200 — assim o teste distingue "passou pelo middleware" de "foi abortado".
func newRouter(allowedOrigins []string) *gin.Engine {
	router := gin.New()
	router.Use(httpmiddleware.CORS(allowedOrigins))
	router.GET("/recurso", func(ginContext *gin.Context) {
		ginContext.JSON(http.StatusOK, gin.H{"ok": true})
	})
	router.OPTIONS("/recurso", func(ginContext *gin.Context) {
		// Se o preflight chegar aqui, o middleware não abortou como devia.
		ginContext.JSON(http.StatusOK, gin.H{"nao_deveria_chegar": true})
	})
	return router
}

func do(router *gin.Engine, method string, origin string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/recurso", nil)
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// TestOrigemPermitida cobre o caminho principal.
func TestOrigemPermitida(t *testing.T) {
	router := newRouter([]string{"https://app.exemplo.com"})

	recorder := do(router, http.MethodGet, "https://app.exemplo.com")

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "https://app.exemplo.com", recorder.Header().Get("Access-Control-Allow-Origin"),
		"a origem é ECOADA, não devolvida como *")
	assert.Contains(t, recorder.Header().Get("Access-Control-Allow-Headers"), "Authorization")
	assert.Equal(t, "Origin", recorder.Header().Get("Vary"))
}

// TestOrigemRecusadaNaoRecebeCabecalho: sem o cabeçalho, o BROWSER bloqueia.
// A requisição em si segue, porque recusar quebraria curl e chamada
// servidor-a-servidor, que não mandam Origin.
func TestOrigemRecusadaNaoRecebeCabecalho(t *testing.T) {
	router := newRouter([]string{"https://app.exemplo.com"})

	recorder := do(router, http.MethodGet, "https://site-malicioso.com")

	require.Equal(t, http.StatusOK, recorder.Code, "a requisição não é abortada")
	assert.Empty(t, recorder.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", recorder.Header().Get("Vary"),
		"Vary sai mesmo na recusa, senão um cache entrega a resposta de uma origem para outra")
}

// TestSemOrigin cobre cliente que não é navegador.
func TestSemOrigin(t *testing.T) {
	router := newRouter([]string{"https://app.exemplo.com"})

	recorder := do(router, http.MethodGet, "")

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Empty(t, recorder.Header().Get("Access-Control-Allow-Origin"))
}

// TestListaVaziaNaoLiberaNada é o teste do DEFAULT: serviço sem configuração
// não fica aberto por omissão.
func TestListaVaziaNaoLiberaNada(t *testing.T) {
	router := newRouter(nil)

	recorder := do(router, http.MethodGet, "https://app.exemplo.com")

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Empty(t, recorder.Header().Get("Access-Control-Allow-Origin"))
}

// TestPreflightTerminaNoMiddleware: se o OPTIONS seguisse, cairia na
// autenticação e voltaria 401 — que o browser mostra como "CORS negado",
// escondendo a causa real.
func TestPreflightTerminaNoMiddleware(t *testing.T) {
	router := newRouter([]string{"https://app.exemplo.com"})

	recorder := do(router, http.MethodOptions, "https://app.exemplo.com")

	require.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Empty(t, recorder.Body.String(), "preflight não tem corpo")
	assert.NotContains(t, recorder.Body.String(), "nao_deveria_chegar")
	assert.Equal(t, "600", recorder.Header().Get("Access-Control-Max-Age"))
}

// TestOrigensComEspacoSaoNormalizadas: a lista vem de variável de ambiente
// separada por vírgula, e "a, b" é escrita comum.
func TestOrigensComEspacoSaoNormalizadas(t *testing.T) {
	router := newRouter([]string{" https://app.exemplo.com ", "", "  "})

	recorder := do(router, http.MethodGet, "https://app.exemplo.com")

	assert.Equal(t, "https://app.exemplo.com", recorder.Header().Get("Access-Control-Allow-Origin"))
}

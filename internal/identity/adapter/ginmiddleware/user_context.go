// Package ginmiddleware é a borda de ENTRADA do bounded context Identity:
// resolve quem está fazendo a requisição e injeta a identidade LOCAL no
// context.Context que atravessa as camadas.
//
// O que atravessa pra dentro é só um uuid. Nem o token, nem o e-mail, nem o
// subject do provedor — quem precisa saber "de quem é esta conta" precisa de
// uma identidade, não de um perfil nem de uma credencial.
package ginmiddleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/platform/httperror"
)

// Header e esquema de autenticação.
const (
	authorizationHeader = "Authorization"
	bearerScheme        = "bearer"
)

// contextKey é um tipo PRIVADO usado como chave de context. String solta
// como chave é colisão esperando acontecer: qualquer package poderia
// escrever na mesma chave sem saber.
type contextKey struct{}

// ResolveUser transforma um token cru na identidade LOCAL do portador.
//
// É um tipo função, não uma interface de um método: em Go a função já é a
// abstração menor possível, e assim o teste passa uma closure em vez de
// declarar um tipo só pra satisfazer contrato. A implementação real vem de
// UserResolverFrom, sobre o use case de sign-in.
type ResolveUser func(ctx context.Context, rawToken string) (uuid.UUID, error)

// Authenticate exige um Bearer token válido, resolve o usuário local e o
// injeta no context da request.
//
// Erro de resolução é traduzido pela MESMA tabela do resto da API
// (httperror), não por status escolhido aqui: token expirado tem que
// responder o mesmo 401/token_expired venha ele deste middleware ou de
// qualquer outro lugar.
func Authenticate(resolve ResolveUser) gin.HandlerFunc {
	return func(ginContext *gin.Context) {
		rawToken, ok := bearerToken(ginContext.GetHeader(authorizationHeader))
		if !ok {
			// Credencial ausente ou mal formada usa o MESMO código de
			// "token inválido": distinguir "não mandou" de "mandou errado"
			// não muda a ação do cliente e só dá informação a quem sonda.
			ginContext.AbortWithStatusJSON(http.StatusUnauthorized, httperror.Body{
				Error: httperror.Detail{Code: "invalid_token", Message: "credencial ausente ou mal formada"},
			})
			return
		}

		userID, err := resolve(ginContext.Request.Context(), rawToken)
		if err != nil {
			status, body := httperror.Translate(err)
			ginContext.AbortWithStatusJSON(status, body)
			return
		}

		// O uuid vai no context da REQUEST (não só no gin.Context) porque é
		// o context.Context que desce pros use cases — eles não conhecem gin.
		ginContext.Request = ginContext.Request.WithContext(
			ContextWithUserID(ginContext.Request.Context(), userID))
		ginContext.Next()
	}
}

// bearerToken extrai o token do header Authorization. O esquema é comparado
// sem diferenciar caixa porque a RFC 7235 o define case-insensitive — e
// cliente mandando "bearer" minúsculo é comum o bastante pra não valer um
// 401 misterioso.
func bearerToken(header string) (string, bool) {
	scheme, credentials, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found {
		return "", false
	}
	if !strings.EqualFold(scheme, bearerScheme) {
		return "", false
	}

	token := strings.TrimSpace(credentials)
	if token == "" {
		return "", false
	}
	return token, true
}

// ContextWithUserID guarda a identidade no context.
//
// Exportada porque a borda de OUTRO bounded context (os testes do
// ginhandler) precisa montar um context autenticado sem forjar um token do
// Google. A chave continua privada — quem não passa por aqui não consegue
// escrever nela, e é isso que importa.
func ContextWithUserID(parent context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(parent, contextKey{}, userID)
}

// UserIDFrom lê a identidade do context. Comma-ok: ausência é possível
// (rota sem o middleware), e o chamador precisa decidir o que fazer.
func UserIDFrom(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(contextKey{}).(uuid.UUID)
	return userID, ok
}

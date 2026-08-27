package ginhandler

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Este arquivo concentra a tradução de QUERY STRING para tipos.
//
// Tudo em query string é texto, e é aqui que ele deixa de ser: uuid, data e
// inteiro nascem tipados antes de entrar no Input do use case. Cada função
// segue a mesma convenção — parâmetro ausente é ausência (nil / zero), não
// erro; parâmetro PRESENTE e inválido é erro de sintaxe (400).
//
// A distinção importa: `?account_id=` vazio significa "não filtrei por
// conta", enquanto `?account_id=abc` significa "quis filtrar e errei o
// formato" — e responder a mesma coisa nos dois casos esconderia o erro do
// cliente.

// optionalUUID lê um uuid opcional da query string.
func optionalUUID(context *gin.Context, name string) (*uuid.UUID, bool) {
	raw := strings.TrimSpace(context.Query(name))
	if raw == "" {
		return nil, true
	}

	parsed, err := uuid.Parse(raw)
	if err != nil {
		respondBadRequest(context, name+" não é um uuid válido")
		return nil, false
	}
	return &parsed, true
}

// optionalTime lê um instante opcional em RFC 3339 (ex:
// 2026-08-01T00:00:00Z). Formato único e explícito de propósito: aceitar
// vários formatos convida ambiguidade (01/02 é fevereiro ou janeiro?).
func optionalTime(context *gin.Context, name string) (time.Time, bool) {
	raw := strings.TrimSpace(context.Query(name))
	if raw == "" {
		return time.Time{}, true
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		respondBadRequest(context, name+" precisa estar em RFC 3339 (ex: 2026-08-01T00:00:00Z)")
		return time.Time{}, false
	}
	return parsed, true
}

// optionalInt lê um inteiro opcional. Ausente devolve zero, que os VOs de
// paginação interpretam como "não pedi nada" — quem decide o default é o
// domínio, não a borda.
func optionalInt(context *gin.Context, name string) (int, bool) {
	raw := strings.TrimSpace(context.Query(name))
	if raw == "" {
		return 0, true
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		respondBadRequest(context, name+" precisa ser um número inteiro")
		return 0, false
	}
	return parsed, true
}

// pathUUID lê um uuid do CAMINHO (não da query string). Diferente dos
// opcionais acima, parâmetro de caminho é sempre obrigatório: a rota não
// existe sem ele.
func pathUUID(context *gin.Context, name string, description string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(context.Param(name))
	if err != nil {
		respondBadRequest(context, description+" não é um uuid válido")
		return uuid.Nil, false
	}
	return parsed, true
}

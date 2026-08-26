package ginhandler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
)

// CategoryHandler expõe as operações de categoria.
type CategoryHandler struct {
	createCategory *application.CreateCategoryUseCase
	logger         *slog.Logger
}

// NewCategoryHandler injeta as dependências por construtor.
func NewCategoryHandler(createCategory *application.CreateCategoryUseCase, logger *slog.Logger) *CategoryHandler {
	return &CategoryHandler{createCategory: createCategory, logger: logger}
}

// createCategoryRequest é o corpo aceito. parent_id chega como STRING (é
// JSON) e o handler converte pra uuid — é a fronteira de tradução: o
// domínio não parseia representação externa.
type createCategoryRequest struct {
	Name     string  `json:"name" binding:"required"`
	ParentID *string `json:"parent_id"`
}

// Create atende POST /v1/categories.
func (h *CategoryHandler) Create(context *gin.Context) {
	var request createCategoryRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		respondBadRequest(context, err.Error())
		return
	}

	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	// Ausente continua ausente (categoria raiz); presente mas inválido é
	// erro de SINTAXE do cliente — 400, não 422.
	var parentID *uuid.UUID
	if request.ParentID != nil {
		parsed, err := uuid.Parse(*request.ParentID)
		if err != nil {
			respondBadRequest(context, "parent_id não é um uuid válido")
			return
		}
		parentID = &parsed
	}

	output, err := h.createCategory.Execute(context.Request.Context(), application.CreateCategoryInput{
		UserID:   userID,
		Name:     request.Name,
		ParentID: parentID,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	context.JSON(http.StatusCreated, toCategoryResponse(output))
}

// categoryResponse é o DTO de saída. parent_id sai como null quando é
// categoria raiz — omitempty seria pior: o cliente não distinguiria "raiz"
// de "campo que a API parou de mandar".
type categoryResponse struct {
	ID       string  `json:"id"`
	UserID   string  `json:"user_id"`
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

// toCategoryResponse traduz o Output do use case no corpo da resposta.
func toCategoryResponse(output application.CreateCategoryOutput) categoryResponse {
	return categoryResponse{
		ID:       output.ID,
		UserID:   output.UserID,
		Name:     output.Name,
		ParentID: output.ParentID,
	}
}

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
	createCategory     *application.CreateCategoryUseCase
	listCategories     *application.ListCategoriesUseCase
	renameCategory     *application.RenameCategoryUseCase
	moveCategory       *application.MoveCategoryUseCase
	addCategoryRule    *application.AddCategoryRuleUseCase
	removeCategoryRule *application.RemoveCategoryRuleUseCase
	logger             *slog.Logger
}

// NewCategoryHandler injeta as dependências por construtor.
func NewCategoryHandler(
	createCategory *application.CreateCategoryUseCase,
	listCategories *application.ListCategoriesUseCase,
	renameCategory *application.RenameCategoryUseCase,
	moveCategory *application.MoveCategoryUseCase,
	addCategoryRule *application.AddCategoryRuleUseCase,
	removeCategoryRule *application.RemoveCategoryRuleUseCase,
	logger *slog.Logger,
) *CategoryHandler {
	return &CategoryHandler{
		createCategory:     createCategory,
		listCategories:     listCategories,
		renameCategory:     renameCategory,
		moveCategory:       moveCategory,
		addCategoryRule:    addCategoryRule,
		removeCategoryRule: removeCategoryRule,
		logger:             logger,
	}
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

// List atende GET /v1/categories.
func (h *CategoryHandler) List(context *gin.Context) {
	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	output, err := h.listCategories.Execute(context.Request.Context(), application.ListCategoriesInput{
		UserID: userID,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	summaries := make([]categorySummaryResponse, 0, len(output.Categories))
	for _, current := range output.Categories {
		summaries = append(summaries, toCategorySummaryResponse(current))
	}

	context.JSON(http.StatusOK, gin.H{"categories": summaries})
}

// categorySummaryResponse é o DTO de leitura de categoria, com suas regras.
type categorySummaryResponse struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	ParentID *string                `json:"parent_id"`
	Rules    []categoryRuleResponse `json:"rules"`
}

// categoryRuleResponse é o DTO de leitura de uma regra.
type categoryRuleResponse struct {
	ID      string `json:"id"`
	Keyword string `json:"keyword"`
}

// renameCategoryRequest é o corpo da renomeação.
type renameCategoryRequest struct {
	Name string `json:"name" binding:"required"`
}

// Rename atende PATCH /v1/categories/:id.
func (h *CategoryHandler) Rename(context *gin.Context) {
	categoryID, ok := pathUUID(context, "id", "id da categoria")
	if !ok {
		return
	}

	var request renameCategoryRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		respondBadRequest(context, err.Error())
		return
	}

	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	summary, err := h.renameCategory.Execute(context.Request.Context(), application.RenameCategoryInput{
		UserID:     userID,
		CategoryID: categoryID,
		Name:       request.Name,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	context.JSON(http.StatusOK, toCategorySummaryResponse(summary))
}

// moveCategoryRequest é o corpo da re-parentagem. `parent_id: null` é
// PEDIDO EXPLÍCITO de virar categoria raiz, e é por isso que o campo é
// ponteiro sem `binding:"required"`: precisamos distinguir "mandou null" de
// "não mandou nada" — o segundo caso é corpo inválido.
type moveCategoryRequest struct {
	ParentID *string `json:"parent_id"`
}

// Move atende PUT /v1/categories/:id/parent.
//
// Sub-recurso `/parent` em vez de mais um campo no PATCH: mover é outra
// intenção do usuário ("reorganizei minha árvore") e merece rota própria,
// igual a `/transactions/:id/category`.
func (h *CategoryHandler) Move(context *gin.Context) {
	categoryID, ok := pathUUID(context, "id", "id da categoria")
	if !ok {
		return
	}

	var request moveCategoryRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		respondBadRequest(context, err.Error())
		return
	}

	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	var parentID *uuid.UUID
	if request.ParentID != nil {
		parsed, err := uuid.Parse(*request.ParentID)
		if err != nil {
			respondBadRequest(context, "parent_id não é um uuid válido")
			return
		}
		parentID = &parsed
	}

	summary, err := h.moveCategory.Execute(context.Request.Context(), application.MoveCategoryInput{
		UserID:     userID,
		CategoryID: categoryID,
		ParentID:   parentID,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	context.JSON(http.StatusOK, toCategorySummaryResponse(summary))
}

// addCategoryRuleRequest é o corpo da criação de regra.
type addCategoryRuleRequest struct {
	Keyword string `json:"keyword" binding:"required"`
}

// AddRule atende POST /v1/categories/:id/rules.
func (h *CategoryHandler) AddRule(context *gin.Context) {
	categoryID, ok := pathUUID(context, "id", "id da categoria")
	if !ok {
		return
	}

	var request addCategoryRuleRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		respondBadRequest(context, err.Error())
		return
	}

	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	summary, err := h.addCategoryRule.Execute(context.Request.Context(), application.AddCategoryRuleInput{
		UserID:     userID,
		CategoryID: categoryID,
		Keyword:    request.Keyword,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	// 201 com a CATEGORIA inteira, não só a regra: a regra não existe fora do
	// aggregate, e devolver o conjunto atualizado evita o cliente ter que
	// reconsultar pra saber como ficou.
	context.JSON(http.StatusCreated, toCategorySummaryResponse(summary))
}

// RemoveRule atende DELETE /v1/categories/:id/rules/:ruleId.
func (h *CategoryHandler) RemoveRule(context *gin.Context) {
	categoryID, ok := pathUUID(context, "id", "id da categoria")
	if !ok {
		return
	}
	ruleID, ok := pathUUID(context, "ruleId", "id da regra")
	if !ok {
		return
	}

	userID, ok := userIDFrom(context)
	if !ok {
		return
	}

	summary, err := h.removeCategoryRule.Execute(context.Request.Context(), application.RemoveCategoryRuleInput{
		UserID:     userID,
		CategoryID: categoryID,
		RuleID:     ruleID,
	})
	if err != nil {
		respondError(context, h.logger, err)
		return
	}

	// 200 com a categoria, não 204: o cliente quer ver o conjunto de regras
	// que sobrou, e 204 o obrigaria a uma segunda chamada.
	context.JSON(http.StatusOK, toCategorySummaryResponse(summary))
}

// toCategorySummaryResponse traduz a projeção do use case.
func toCategorySummaryResponse(summary application.CategorySummary) categorySummaryResponse {
	rules := make([]categoryRuleResponse, 0, len(summary.Rules))
	for _, rule := range summary.Rules {
		rules = append(rules, categoryRuleResponse{ID: rule.ID, Keyword: rule.Keyword})
	}

	return categorySummaryResponse{
		ID:       summary.ID,
		Name:     summary.Name,
		ParentID: summary.ParentID,
		Rules:    rules,
	}
}

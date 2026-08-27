package application

import (
	"context"
	"time"

	"github.com/google/uuid"

	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

// ViewProfileInput é a entrada da consulta. O UserID chega como uuid JÁ
// parseado pela borda (o middleware o resolveu do token) — o domínio não
// parseia representação externa.
//
// Repare que a entrada é o usuário AUTENTICADO, não um id arbitrário de
// caminho: não existe "ver o perfil de outra pessoa" nesta API, então nem
// existe o parâmetro que permitiria pedir isso. É a forma mais barata de
// não ter uma falha de autorização — a que não pode ser expressa.
type ViewProfileInput struct {
	UserID uuid.UUID
}

// ViewProfileOutput é a projeção serializável do perfil.
type ViewProfileOutput struct {
	UserID string
	Email  string
	Name   string

	// AvatarURL vazio = usuário sem avatar. A borda HTTP decide como
	// representar ausência no JSON (null), que é vocabulário dela.
	AvatarURL string

	RegisteredAt time.Time
}

// ViewProfileUseCase devolve o perfil do usuário autenticado.
//
// É o primeiro use case de CONSULTA do projeto. Ele existe (em vez de o
// handler falar direto com o repositório) por duas razões: mantém a regra
// "toda ação é um use case", e é ele que decide o que o perfil expõe — o
// aggregate tem a identidade externa, e ela NÃO sai daqui.
type ViewProfileUseCase struct {
	users identity.Repository
}

// NewViewProfileUseCase injeta as dependências por construtor.
func NewViewProfileUseCase(users identity.Repository) *ViewProfileUseCase {
	return &ViewProfileUseCase{users: users}
}

// Execute carrega o usuário e projeta o perfil.
func (u *ViewProfileUseCase) Execute(ctx context.Context, input ViewProfileInput) (ViewProfileOutput, error) {
	userID, err := identity.UserIDFromUUID(input.UserID)
	if err != nil {
		return ViewProfileOutput{}, err
	}

	found, err := u.users.FindByID(ctx, userID)
	if err != nil {
		return ViewProfileOutput{}, err
	}

	return toViewProfileOutput(found), nil
}

// toViewProfileOutput projeta o aggregate no DTO de saída.
//
// O que fica FORA é a decisão: a identidade externa (provedor + subject) não
// entra. Ela é detalhe de como a pessoa se autentica, não parte do perfil, e
// devolvê-la exporia o subject do Google a qualquer cliente — inclusive a um
// frontend que o registraria em log ou analytics.
func toViewProfileOutput(user *identity.User) ViewProfileOutput {
	userID := user.ID()
	email := user.Email()
	avatar := user.Avatar()

	return ViewProfileOutput{
		UserID:       userID.String(),
		Email:        email.String(),
		Name:         user.Name(),
		AvatarURL:    avatar.String(),
		RegisteredAt: user.RegisteredAt(),
	}
}

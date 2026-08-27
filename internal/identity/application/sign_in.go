// Package application reúne os casos de uso do bounded context Identity.
// Mesmo layout do Financial Tracking: um package coeso, um arquivo por use
// case, com Input/Output e mapeamento colados no use case que os define.
package application

import (
	"context"
	"errors"
	"strings"

	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

// SignInInput é a entrada: o token cru como veio no header.
//
// O use case NÃO recebe e-mail nem nome do cliente — só o token. Aceitar
// esses campos seria aceitar que o chamador diga quem ele é; aqui quem diz é
// o provedor de identidade, depois de a assinatura do token ser verificada.
type SignInInput struct {
	RawToken string
}

// SignInOutput é a projeção do resultado. UserID é o uuid LOCAL — é ele que
// atravessa a fronteira para os outros bounded contexts, nunca o subject do
// provedor.
type SignInOutput struct {
	UserID     string
	Email      string
	Name       string
	Registered bool // true no primeiro acesso
}

// SignInUseCase autentica o portador de um token do provedor de identidade e
// resolve quem ele é AQUI DENTRO, provisionando o usuário no primeiro
// acesso.
//
// O provisionamento just-in-time é uma decisão: a alternativa seria um
// endpoint de cadastro separado, que criaria um estado impossível de
// sustentar — token válido de uma pessoa que o sistema não conhece. Como a
// autenticação é do provedor, "existir no Google e não existir aqui" só pode
// significar "primeiro acesso".
type SignInUseCase struct {
	users      identity.Repository
	verifier   identity.TokenVerifier
	dispatcher events.Dispatcher
}

// NewSignInUseCase injeta as dependências por construtor.
func NewSignInUseCase(users identity.Repository, verifier identity.TokenVerifier, dispatcher events.Dispatcher) *SignInUseCase {
	return &SignInUseCase{users: users, verifier: verifier, dispatcher: dispatcher}
}

// Execute verifica o token, constrói os VOs e resolve o usuário local:
// registra no primeiro acesso, sincroniza o perfil nos seguintes.
func (u *SignInUseCase) Execute(ctx context.Context, input SignInInput) (SignInOutput, error) {
	verified, err := u.verifier.Verify(ctx, input.RawToken)
	if err != nil {
		return SignInOutput{}, err
	}

	// O provedor manda o e-mail E a atestação de que ele é da pessoa.
	// Aceitar e-mail não verificado deixaria alguém registrar-se com o
	// endereço de outro e herdar o que o sistema venha a associar a e-mail
	// (convite, compartilhamento, recuperação).
	if !verified.EmailVerified {
		return SignInOutput{}, identity.ErrEmailNotVerified
	}

	external, err := identity.NewExternalIdentity(verified.Provider, verified.Subject)
	if err != nil {
		return SignInOutput{}, err
	}

	email, err := identity.NewEmail(verified.Email)
	if err != nil {
		return SignInOutput{}, err
	}

	name := resolveName(verified.Name, email)
	avatar := resolveAvatar(verified.AvatarURL)

	existing, err := u.users.FindByExternalIdentity(ctx, external)
	if errors.Is(err, identity.ErrNotFound) {
		return u.register(ctx, email, name, avatar, external)
	}
	if err != nil {
		return SignInOutput{}, err
	}

	return u.signInExisting(ctx, existing, email, name, avatar)
}

// register provisiona o usuário no primeiro acesso e publica Registered.
func (u *SignInUseCase) register(ctx context.Context, email identity.Email, name string, avatar identity.AvatarURL, external identity.ExternalIdentity) (SignInOutput, error) {
	registered, err := identity.Register(email, name, avatar, external)
	if err != nil {
		return SignInOutput{}, err
	}

	if err := u.users.Save(ctx, registered); err != nil {
		return SignInOutput{}, err
	}

	// Save → Dispatch → ClearEvents, a mesma ordem do spec §6.
	recorded := registered.Events()
	if err := u.dispatcher.Dispatch(ctx, recorded...); err != nil {
		return SignInOutput{}, err
	}
	registered.ClearEvents()

	return toSignInOutput(registered, true), nil
}

// signInExisting sincroniza o perfil se o provedor trouxe algo diferente.
//
// Só grava quando MUDOU: o perfil chega em todo login, e persistir sempre
// seria uma escrita por request sem fato novo — a decisão de "mudou?" mora
// no aggregate (SyncProfile), não aqui.
func (u *SignInUseCase) signInExisting(ctx context.Context, existing *identity.User, email identity.Email, name string, avatar identity.AvatarURL) (SignInOutput, error) {
	changed, err := existing.SyncProfile(email, name, avatar)
	if err != nil {
		return SignInOutput{}, err
	}

	if changed {
		if err := u.users.Save(ctx, existing); err != nil {
			return SignInOutput{}, err
		}
	}

	return toSignInOutput(existing, false), nil
}

// resolveName decide o nome quando o provedor não manda um.
//
// O claim `name` do Google depende dos escopos concedidos, então ele é
// OPCIONAL na prática. Recusar o login por falta de um dado de exibição
// seria desproporcional; a parte local do e-mail é um default honesto e o
// próximo login corrige se o claim aparecer.
func resolveName(providedName string, email identity.Email) string {
	if strings.TrimSpace(providedName) != "" {
		return providedName
	}

	localPart, _, _ := strings.Cut(email.String(), "@")
	return localPart
}

// resolveAvatar constrói o VO do avatar, tratando URL inválida como AUSENTE.
//
// Diferente do e-mail, avatar torto não impede o login: é dado cosmético, e
// recusar a entrada de alguém porque o provedor mandou uma URL estranha seria
// desproporcional. O próximo login corrige se a URL voltar ao normal.
func resolveAvatar(rawURL string) identity.AvatarURL {
	avatar, err := identity.NewAvatarURL(rawURL)
	if err != nil {
		return identity.AvatarURL{}
	}
	return avatar
}

// toSignInOutput projeta o aggregate no DTO de saída.
func toSignInOutput(user *identity.User, registered bool) SignInOutput {
	userID := user.ID()
	email := user.Email()

	return SignInOutput{
		UserID:     userID.String(),
		Email:      email.String(),
		Name:       user.Name(),
		Registered: registered,
	}
}

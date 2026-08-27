package identity

import "context"

// VerifiedIdentity é o que o provedor de identidade afirma sobre quem está
// chamando, depois de o token ter sido validado.
//
// Campos exportados porque é contrato de TRANSPORTE na fronteira, não modelo
// de domínio — mesma natureza do openfinance.ProviderTransaction. Quem tem
// invariante é o aggregate; o use case constrói os VOs a partir daqui.
type VerifiedIdentity struct {
	// Provider e Subject formam a âncora do usuário (viram
	// ExternalIdentity).
	Provider string
	Subject  string

	// Email, Name e AvatarURL são PERFIL, e vêm do provedor a cada login.
	Email string
	Name  string

	// AvatarURL é o claim `picture`. Vazio é normal: depende do escopo
	// `profile` ter sido concedido e de a conta ter foto.
	AvatarURL string

	// EmailVerified é a atestação do provedor de que aquele e-mail é da
	// pessoa. Vem separado do Email de propósito: sem esse campo, o use
	// case teria que confiar num e-mail que ninguém garantiu, e o domínio
	// não teria como recusar.
	EmailVerified bool
}

// TokenVerifier é a porta de verificação de token do provedor de
// identidade. Interface no domínio, implementação (Google) no adapter.
//
// UM método, de propósito (ISP): o v1 valida token de acesso já obtido pelo
// cliente. Fluxo de consentimento, refresh e revogação entram como métodos
// ou portas próprias quando existirem — obrigar o fake de teste a
// implementar o que ninguém chama é como interface "Deus" começa.
//
// DDD: Port (integração externa) — contrato no domain, adapter na
// infraestrutura.
type TokenVerifier interface {
	// Verify valida o token e devolve o que o provedor afirma. Token
	// inválido → ErrInvalidToken; expirado → ErrTokenExpired (o cliente
	// pode renovar e tentar de novo).
	Verify(ctx context.Context, rawToken string) (VerifiedIdentity, error)
}

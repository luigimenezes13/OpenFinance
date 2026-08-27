package identity

import "errors"

// Sentinels do bounded context Identity. O prefixo "identity:" identifica a
// origem quando o erro sobe embrulhado pelas camadas.
var (
	// ErrNotFound indica usuário inexistente. É erro ESPERADO no fluxo de
	// login: é ele que dispara o provisionamento do primeiro acesso.
	ErrNotFound = errors.New("identity: user not found")

	// ErrInvalidID indica um UserID que não identifica nada (uuid.Nil).
	ErrInvalidID = errors.New("identity: invalid id")

	// ErrInvalidEmail indica e-mail estruturalmente inválido.
	ErrInvalidEmail = errors.New("identity: invalid email")

	// ErrInvalidName indica nome vazio ou só com espaços.
	ErrInvalidName = errors.New("identity: invalid name")

	// ErrInvalidExternalIdentity indica provedor desconhecido ou subject
	// ausente.
	ErrInvalidExternalIdentity = errors.New("identity: invalid external identity")

	// ErrInvalidRegisteredAt indica instante de registro ausente — só
	// alcançável por linha corrompida no banco, já que Register sempre
	// data o usuário.
	ErrInvalidRegisteredAt = errors.New("identity: invalid registered at")

	// ErrEmailNotVerified indica que o provedor entregou um e-mail que ele
	// mesmo NÃO atesta como verificado. Recusar é decisão de segurança:
	// aceitar deixaria alguém registrar-se com o e-mail de outra pessoa e
	// herdar qualquer coisa que o sistema venha a associar a e-mail.
	ErrEmailNotVerified = errors.New("identity: email not verified by provider")

	// ErrInvalidToken indica token ausente, mal formado, com assinatura
	// inválida ou com audience/issuer errados. O adapter do provedor traduz
	// as falhas específicas dele para este sentinel — a borda não precisa
	// distinguir "assinatura ruim" de "aud errado", e detalhar isso na
	// resposta ajudaria quem está tentando forjar token.
	ErrInvalidToken = errors.New("identity: invalid token")

	// ErrTokenExpired é separado porque o cliente PODE agir: renovar o
	// token e tentar de novo.
	ErrTokenExpired = errors.New("identity: token expired")
)

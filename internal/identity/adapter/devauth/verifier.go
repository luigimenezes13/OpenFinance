//go:build devauth

// Package devauth é um verificador de token FALSO, para desenvolvimento e
// teste manual sem frontend. Ele aceita como "token" um e-mail e devolve uma
// identidade sintética — ou seja, quem chamar a API escolhe quem quer ser.
//
// # POR QUE ISTO É UMA BUILD TAG E NÃO UMA VARIÁVEL DE AMBIENTE
//
// Uma flag de ambiente que desliga autenticação é uma das formas mais
// conhecidas de incidente: basta um valor errado num deploy, um Helm chart
// copiado, um docker-compose de produção herdado do de desenvolvimento.
//
// Com build tag, o binário de produção NÃO CONTÉM este código. `go build
// ./cmd/api` sem `-tags devauth` não compila nada daqui, e qualquer
// referência a este package quebra o build. Não existe configuração,
// deploy ou variável capaz de ativá-lo em produção — o caminho é físico,
// não lógico.
//
// Uso: `make run-devauth`.
package devauth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

// Verifier trata o token recebido como o e-mail do usuário.
type Verifier struct {
	logger *slog.Logger
}

// Garante em tempo de compilação que satisfaz a porta.
var _ identity.TokenVerifier = (*Verifier)(nil)

// NewVerifier cria o verificador de desenvolvimento, gritando no log.
func NewVerifier(logger *slog.Logger) *Verifier {
	logger.Warn("AUTENTICAÇÃO FALSA ATIVA (build tag devauth) — qualquer e-mail no header Authorization vira um usuário; NUNCA use este binário fora da sua máquina")
	return &Verifier{logger: logger}
}

// Verify aceita `Bearer <email>` e devolve uma identidade sintética.
//
// O subject é derivado do e-mail com um prefixo `devauth:`, então os
// usuários criados por aqui são distinguíveis dos reais no banco — e um
// login real do Google com o mesmo e-mail cria OUTRO usuário, em vez de
// assumir este. Sem o prefixo, dado de teste e dado real se misturariam
// pela chave de identidade.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (identity.VerifiedIdentity, error) {
	email := strings.TrimSpace(rawToken)
	if email == "" || !strings.Contains(email, "@") {
		return identity.VerifiedIdentity{}, fmt.Errorf("devauth: use um e-mail como token: %w", identity.ErrInvalidToken)
	}

	// Aviso a CADA requisição, de propósito: se este binário rodar onde não
	// devia, o log denuncia sozinho.
	v.logger.WarnContext(ctx, "autenticação falsa em uso", slog.String("email", email))

	localPart, _, _ := strings.Cut(email, "@")
	return identity.VerifiedIdentity{
		Provider: identity.ProviderGoogle,
		Subject:  "devauth:" + email,
		Email:    email,
		Name:     localPart,
		// Avatar sintético apontando pra um domínio RESERVADO pra exemplo
		// (RFC 2606): a URL é válida e nunca vai carregar imagem de
		// terceiro nem bater em servidor real de ninguém.
		AvatarURL:     "https://example.com/avatars/" + localPart + ".png",
		EmailVerified: true,
	}, nil
}

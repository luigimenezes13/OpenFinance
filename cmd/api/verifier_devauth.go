//go:build devauth

package main

import (
	"log/slog"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/devauth"
	identitydomain "github.com/luigimenezes13/financial-manager/internal/identity/domain"
	"github.com/luigimenezes13/financial-manager/internal/platform/config"
)

// newTokenVerifier devolve o verificador FALSO de desenvolvimento.
//
// Só existe quando o binário é compilado com `-tags devauth`. Sem a tag,
// este arquivo não entra no build e o package devauth também não — não há
// configuração capaz de ativá-lo em produção.
func newTokenVerifier(_ config.Config, logger *slog.Logger) (identitydomain.TokenVerifier, error) {
	return devauth.NewVerifier(logger), nil
}

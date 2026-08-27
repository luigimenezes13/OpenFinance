//go:build !devauth

package main

import (
	"log/slog"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/googleoidc"
	identitydomain "github.com/luigimenezes13/financial-manager/internal/identity/domain"
	"github.com/luigimenezes13/financial-manager/internal/platform/config"
)

// newTokenVerifier devolve o verificador REAL (Google OIDC). É esta a versão
// compilada em qualquer build normal — a alternativa de desenvolvimento só
// existe sob a build tag `devauth`, e não está presente neste binário.
func newTokenVerifier(configuration config.Config, _ *slog.Logger) (identitydomain.TokenVerifier, error) {
	return googleoidc.NewVerifier(configuration.GoogleClientID)
}

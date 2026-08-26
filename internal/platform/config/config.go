// Package config lê a configuração do ambiente. Fica em platform/ porque
// não é bounded context — é encanamento.
//
// Stdlib pura (os.Getenv), como o spec §2 previu: envconfig entra quando
// ficar repetitivo, não antes.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config é a configuração do processo. Sem valores default para segredo:
// DATABASE_URL ausente é erro, não localhost implícito — default silencioso
// de banco é como um serviço de produção acaba conectado na máquina de
// alguém.
type Config struct {
	DatabaseURL     string
	HTTPAddr        string
	ShutdownTimeout time.Duration
}

const (
	defaultHTTPAddr        = ":8080"
	defaultShutdownTimeout = 10 * time.Second
)

// Load lê e valida o ambiente.
func Load() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL é obrigatória")
	}

	return Config{
		DatabaseURL:     databaseURL,
		HTTPAddr:        valueOr(os.Getenv("HTTP_ADDR"), defaultHTTPAddr),
		ShutdownTimeout: defaultShutdownTimeout,
	}, nil
}

// valueOr devolve o fallback quando a variável não foi definida.
func valueOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

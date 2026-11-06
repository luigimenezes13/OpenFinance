// Package config lê a configuração do ambiente. Fica em platform/ porque
// não é bounded context — é encanamento.
//
// Stdlib pura (os.Getenv), como o spec §2 previu: envconfig entra quando
// ficar repetitivo, não antes.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config é a configuração do processo. Sem valores default para segredo:
// DATABASE_URL ausente é erro, não localhost implícito — default silencioso
// de banco é como um serviço de produção acaba conectado na máquina de
// alguém.
type Config struct {
	DatabaseURL string

	// GoogleClientID é a audience esperada nos ID tokens. Obrigatória, e por
	// um motivo de segurança: sem audience, o serviço aceitaria token
	// legítimo do Google emitido pra OUTRO aplicativo. Falhar no boot é
	// melhor que subir aceitando qualquer token.
	GoogleClientID string

	// CORSAllowedOrigins são as origens que o navegador pode usar pra chamar
	// esta API (ex: "https://app.exemplo.com,http://localhost:3000"). Vazio =
	// nenhum cabeçalho CORS, que é o default restrito: serviço sem
	// configuração não deve ficar aberto por omissão.
	CORSAllowedOrigins []string

	HTTPAddr        string
	ShutdownTimeout time.Duration

	OutboxMaxWorkers int
	OutboxJobTimeout time.Duration
}

const (
	defaultHTTPAddr         = ":8080"
	defaultShutdownTimeout  = 10 * time.Second
	defaultOutboxMaxWorkers = 10
	defaultOutboxJobTimeout = 30 * time.Second
)

// Load lê e valida o ambiente.
func Load() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL é obrigatória")
	}

	googleClientID := os.Getenv("GOOGLE_CLIENT_ID")
	if googleClientID == "" {
		return Config{}, fmt.Errorf("config: GOOGLE_CLIENT_ID é obrigatória")
	}

	outboxMaxWorkers, err := positiveIntOr("OUTBOX_MAX_WORKERS", defaultOutboxMaxWorkers)
	if err != nil {
		return Config{}, err
	}
	outboxJobTimeout, err := positiveDurationOr("OUTBOX_JOB_TIMEOUT", defaultOutboxJobTimeout)
	if err != nil {
		return Config{}, err
	}

	return Config{
		DatabaseURL:        databaseURL,
		GoogleClientID:     googleClientID,
		CORSAllowedOrigins: splitAndTrim(os.Getenv("CORS_ALLOWED_ORIGINS")),
		HTTPAddr:           valueOr(os.Getenv("HTTP_ADDR"), defaultHTTPAddr),
		ShutdownTimeout:    defaultShutdownTimeout,
		OutboxMaxWorkers:   outboxMaxWorkers,
		OutboxJobTimeout:   outboxJobTimeout,
	}, nil
}

// splitAndTrim quebra uma lista separada por vírgula, descartando vazios —
// "a, b," é escrita comum em variável de ambiente.
func splitAndTrim(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

// valueOr devolve o fallback quando a variável não foi definida.
func valueOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func positiveIntOr(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("config: %s must be a positive integer, got %q", name, raw)
	}
	return value, nil
}

func positiveDurationOr(name string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("config: %s must be a positive duration like 30s, got %q", name, raw)
	}
	return value, nil
}

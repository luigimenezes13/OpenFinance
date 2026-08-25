package openfinance

import "errors"

// Sentinels da fronteira Open Finance. O domínio expõe erros tipados; a
// borda HTTP traduz cada um via errors.Is.
var (
	// ErrAccountNotConnected indica tentativa de importar transações para
	// uma conta MANUAL — não existe contraparte no provider pra consultar.
	ErrAccountNotConnected = errors.New("openfinance: account is not connected to a provider")

	// ErrProviderMismatch indica que a conta pertence a outro provider que
	// não o adapter injetado (conta da Pluggy, adapter da Belvo).
	ErrProviderMismatch = errors.New("openfinance: account belongs to another provider")

	// ErrProviderUnavailable é o erro que o ADAPTER usa pra traduzir falha
	// transitória do provider (rede, 5xx, rate limit). Declarado aqui pro
	// use case e a borda HTTP poderem reagir sem conhecer o adapter.
	ErrProviderUnavailable = errors.New("openfinance: provider unavailable")

	// ErrConsentExpired é o erro que o ADAPTER usa quando o consentimento
	// caiu e o usuário precisa reconectar (doc §6.3).
	ErrConsentExpired = errors.New("openfinance: consent expired, reconnect required")
)

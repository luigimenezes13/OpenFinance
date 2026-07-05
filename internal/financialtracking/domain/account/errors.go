package account

import "errors"

// Sentinels do aggregate Account. O prefixo "account:" na mensagem
// identifica a origem quando o erro sobe embrulhado pelas camadas.
var (
	ErrNotFound         = errors.New("account: not found")
	ErrInvalidID        = errors.New("account: invalid id")
	ErrStaleBalance     = errors.New("account: stale balance")
	ErrInvalidName      = errors.New("account: invalid name")
	ErrInvalidKind      = errors.New("account: invalid kind")
	ErrInvalidSource    = errors.New("account: invalid source")
	ErrInvalidBalance   = errors.New("account: invalid balance")
	ErrCurrencyMismatch = errors.New("account: currency mismatch")
)

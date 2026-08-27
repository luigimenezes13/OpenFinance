package transaction

import "errors"

// Sentinels do aggregate Transaction.
var (
	ErrNotFound           = errors.New("transaction: not found")
	ErrInvalidID          = errors.New("transaction: invalid id")
	ErrInvalidMoney       = errors.New("transaction: invalid money")
	ErrZeroMoney          = errors.New("transaction: amount must be non-zero")
	ErrInvalidOccurredAt  = errors.New("transaction: invalid occurred at")
	ErrInvalidDescription = errors.New("transaction: invalid description")
	ErrInvalidRef         = errors.New("transaction: invalid external ref")
	ErrInvalidAssignment  = errors.New("transaction: invalid category assignment")
	ErrNotReconcilable    = errors.New("transaction: manual transaction cannot be reconciled")
	ErrInvalidPeriod      = errors.New("transaction: invalid period")
)

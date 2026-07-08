package category

import "errors"

// Sentinels do aggregate Category. Os demais (ErrNotFound, ErrInvalidName,
// ErrDuplicateRule) chegam junto com o aggregate completo.
var ErrInvalidID = errors.New("category: invalid id")

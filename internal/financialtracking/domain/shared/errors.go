package shared

import "errors"

// ErrForbidden indica que o usuário autenticado tentou operar um aggregate
// que pertence a outro usuário (violação de ownership). A borda HTTP traduz
// este sentinel para 403 — o domínio não sabe o que é HTTP.
var ErrForbidden = errors.New("forbidden")

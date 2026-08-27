package kernel

import "errors"

// ErrInvalidIdentifier indica um UUID que não identifica nada: o uuid.Nil.
// Cada bounded context traduz este sentinel genérico para o seu próprio
// (ex: account.ErrInvalidID) no construtor de identidade do aggregate.
var ErrInvalidIdentifier = errors.New("invalid identifier")

// ErrInvalidPage indica janela de paginação inválida: limite negativo, acima
// do teto, ou deslocamento negativo.
var ErrInvalidPage = errors.New("invalid page")

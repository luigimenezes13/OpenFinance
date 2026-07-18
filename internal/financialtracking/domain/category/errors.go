package category

import "errors"

// Sentinels do aggregate Category. O domínio expõe erros tipados; a borda
// HTTP (httperror) traduz cada um pro status certo via errors.Is.
var (
	// ErrInvalidID indica um CategoryID que não identifica nada (uuid.Nil).
	ErrInvalidID = errors.New("category: invalid id")

	// ErrInvalidRuleID indica um RuleID que não identifica nada (uuid.Nil).
	ErrInvalidRuleID = errors.New("category: invalid rule id")

	// ErrInvalidName indica nome de categoria vazio ou só com espaços.
	ErrInvalidName = errors.New("category: invalid name")

	// ErrInvalidParent indica pai inválido: zero value, ou a própria
	// categoria como pai de si mesma.
	ErrInvalidParent = errors.New("category: invalid parent")

	// ErrInvalidRule indica tentativa de adicionar uma regra zero value.
	ErrInvalidRule = errors.New("category: invalid rule")

	// ErrInvalidKeyword indica keyword de regra vazio ou só com espaços.
	ErrInvalidKeyword = errors.New("category: invalid keyword")

	// ErrDuplicateRule indica regra com keyword já existente na categoria.
	ErrDuplicateRule = errors.New("category: duplicate rule")

	// ErrRuleNotFound indica remoção de uma regra que não existe.
	ErrRuleNotFound = errors.New("category: rule not found")

	// ErrNotFound indica categoria inexistente (traduzido pelo repository a
	// partir de pgx.ErrNoRows — erro de driver não vaza pro domínio).
	ErrNotFound = errors.New("category: not found")
)

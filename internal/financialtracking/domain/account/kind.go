package account

// Kind classifica a natureza da conta (corrente, poupança, cartão).
// Conjunto fechado: fora do package só se cria Kind pelas instâncias
// abaixo ou pelo NewKind.
//
// DDD: Value Object (enum) — conjunto fechado, imutável.
type Kind struct {
	value string
}

// Instâncias válidas de Kind. Mapeiam 1:1 com os subtypes da Pluggy
// (CHECKING_ACCOUNT, SAVINGS_ACCOUNT, CREDIT_CARD).
var (
	KindChecking   = Kind{value: "checking"}
	KindSavings    = Kind{value: "savings"}
	KindCreditCard = Kind{value: "credit_card"}
)

// supportedKinds indexa as instâncias pela forma textual sem duplicar
// literais.
var supportedKinds = map[string]Kind{
	KindChecking.value:   KindChecking,
	KindSavings.value:    KindSavings,
	KindCreditCard.value: KindCreditCard,
}

// NewKind reconstrói um Kind a partir da forma textual. Valor desconhecido
// → ErrInvalidKind.
func NewKind(raw string) (Kind, error) {
	kind, ok := supportedKinds[raw]
	if !ok {
		return Kind{}, ErrInvalidKind
	}
	return kind, nil
}

// String retorna a forma textual (é o que o repository persiste).
func (k Kind) String() string {
	return k.value
}

// IsZero informa se este Kind foi criado fora dos caminhos válidos.
func (k Kind) IsZero() bool {
	return k.value == ""
}

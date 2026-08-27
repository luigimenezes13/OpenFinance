package identity

import "strings"

// Email é o VO de e-mail do usuário. Imutável, auto-validado, guardado em
// forma canônica (trim + lowercase) — assim "Luigi@Gmail.com" e
// "luigi@gmail.com" são o MESMO e-mail para comparação e unicidade.
//
// A validação aqui é ESTRUTURAL de propósito (tem parte local, tem @, tem
// domínio com ponto). Não existe regex que decida se um e-mail existe, e
// tentar isso produz o pior dos mundos: recusa endereço válido e aceita
// inexistente. Quem atesta que o e-mail é real é o provedor de identidade
// (o claim email_verified do Google) — o domínio confia nessa atestação em
// vez de reimplementá-la.
//
// DDD: Value Object — imutável, sem identidade, auto-validado.
type Email struct {
	value string
}

// NewEmail valida e constrói o e-mail na forma canônica.
func NewEmail(raw string) (Email, error) {
	canonical := strings.ToLower(strings.TrimSpace(raw))
	if !isStructurallyValid(canonical) {
		return Email{}, ErrInvalidEmail
	}
	return Email{value: canonical}, nil
}

// isStructurallyValid checa a forma mínima: local@dominio.tld, sem espaço.
func isStructurallyValid(candidate string) bool {
	if candidate == "" || strings.ContainsAny(candidate, " \t\n") {
		return false
	}

	local, domain, found := strings.Cut(candidate, "@")
	if !found || local == "" || domain == "" {
		return false
	}
	// Recusa "a@b@c": Cut pega só o primeiro @, então sobra um no domínio.
	if strings.Contains(domain, "@") {
		return false
	}

	label, topLevel, hasDot := strings.Cut(domain, ".")
	return hasDot && label != "" && topLevel != ""
}

// String retorna a forma canônica (é o que o repository persiste).
func (e Email) String() string {
	return e.value
}

// Equals compara e-mails por valor.
func (e Email) Equals(other Email) bool {
	return e.value == other.value
}

// IsZero informa se este Email foi criado fora do construtor.
func (e Email) IsZero() bool {
	return e.value == ""
}

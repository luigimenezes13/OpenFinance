package identity

import (
	"net/url"
	"strings"
)

// AvatarURL é o VO da foto de perfil do usuário, como o provedor a informa
// (claim `picture` do Google).
//
// É OPCIONAL: o zero value significa "sem avatar", e é estado legítimo — o
// claim depende do escopo `profile` ter sido concedido, e nem toda conta tem
// foto. Por isso o VO tem IsZero e o aggregate aceita o zero value, ao
// contrário de Email.
//
// Guardamos a URL, não a imagem. Duas consequências assumidas: a URL do
// Google pode ROTACIONAR (a foto some sem o nosso banco saber — o próximo
// login corrige, porque o perfil é sincronizado a cada entrada), e o
// carregamento da imagem é do cliente, direto do CDN do Google. Servir a
// imagem por aqui exigiria baixar, armazenar e expirar arquivo de terceiro,
// o que é outro problema (e outro custo) sem ganho no v1.
//
// DDD: Value Object — imutável, sem identidade, auto-validado.
type AvatarURL struct {
	value string
}

// Esquemas aceitos. Conjunto fechado por segurança: sem essa restrição,
// `javascript:` ou `data:` entrariam aqui e o frontend renderizaria o que
// veio de fora num atributo src — é XSS entregue pela API.
var supportedAvatarSchemes = map[string]struct{}{
	"http":  {},
	"https": {},
}

// NewAvatarURL valida e constrói a URL do avatar. Exige URL ABSOLUTA com
// esquema http/https e host — relativa não faz sentido aqui, porque a imagem
// vive em outro domínio.
func NewAvatarURL(raw string) (AvatarURL, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return AvatarURL{}, ErrInvalidAvatarURL
	}

	parsed, err := url.Parse(candidate)
	if err != nil {
		return AvatarURL{}, ErrInvalidAvatarURL
	}
	if _, ok := supportedAvatarSchemes[strings.ToLower(parsed.Scheme)]; !ok {
		return AvatarURL{}, ErrInvalidAvatarURL
	}
	if parsed.Host == "" {
		return AvatarURL{}, ErrInvalidAvatarURL
	}

	return AvatarURL{value: candidate}, nil
}

// String retorna a URL (vazio quando não há avatar).
func (a AvatarURL) String() string {
	return a.value
}

// Equals compara avatares por valor.
func (a AvatarURL) Equals(other AvatarURL) bool {
	return a.value == other.value
}

// IsZero informa se o usuário não tem avatar.
func (a AvatarURL) IsZero() bool {
	return a.value == ""
}

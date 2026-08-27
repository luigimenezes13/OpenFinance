// Package identity contém o domínio do bounded context Identity: o
// aggregate User, os VOs de e-mail e de referência ao provedor externo, o
// evento de registro e as portas (repositório e verificador de token).
//
// A fronteira com o resto do sistema é ESTREITA de propósito: os outros
// bounded contexts conhecem apenas o uuid do usuário. Nem o e-mail, nem o
// provedor, nem o subject do Google atravessam — quem precisa saber "de
// quem é esta conta" precisa de uma identidade, não do perfil.
//
// Repare que este package tem o seu PRÓPRIO UserID, distinto do
// shared.UserID do Financial Tracking. Os dois carregam um uuid e ainda
// assim são tipos diferentes, e isso é a decisão certa: são conceitos de
// contextos diferentes que por acaso compartilham a representação. A
// tradução acontece na borda (middleware), em uuid puro — que é o único
// vocabulário comum entre contextos.
package identity

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/kernel"
)

// userIdentity é o phantom type que marca identidades de usuário deste BC.
type userIdentity struct{}

// UserID é o VO de identidade do usuário.
//
// DDD: Value Object (identidade) — imutável, auto-validado no construtor.
type UserID = kernel.TypedID[userIdentity]

// NewUserID gera uma identidade nova.
func NewUserID() UserID {
	return kernel.NewTypedID[userIdentity]()
}

// UserIDFromUUID constrói o UserID a partir de um uuid.UUID já convertido
// pela borda, traduzindo a invariante pro sentinel deste BC.
func UserIDFromUUID(value uuid.UUID) (UserID, error) {
	userID, err := kernel.TypedIDFromUUID[userIdentity](value)
	if err != nil {
		return UserID{}, ErrInvalidID
	}
	return userID, nil
}

// User é o aggregate root do BC Identity.
//
// Não guarda senha, nem hash, nem token: a autenticação é DELEGADA ao
// provedor externo. O que este aggregate sabe é "quem é esta pessoa aqui
// dentro e a que identidade externa ela corresponde" — e é justamente por
// não guardar credencial que o vazamento deste banco não vaza login de
// ninguém.
//
// DDD: Aggregate Root — fronteira de consistência; toda mutação e toda
// emissão de eventos passa por aqui.
type User struct {
	kernel.EventRecorder // embed: promove Events(), ClearEvents(), RecordEvent()

	id           UserID
	email        Email
	name         string
	avatar       AvatarURL // zero value = sem avatar
	external     ExternalIdentity
	registeredAt time.Time
}

// Register cria o usuário no PRIMEIRO acesso e emite Registered.
//
// O nome do método é do negócio, não técnico: o fato é "um usuário se
// registrou", e é isso que o evento carrega. `New` diria menos.
// O avatar entra como parâmetro (e não por um setter depois) porque é dado
// que chega junto no login: construir o usuário sem ele e completar em
// seguida criaria um instante em que o aggregate existe incompleto.
func Register(email Email, name string, avatar AvatarURL, external ExternalIdentity) (*User, error) {
	if email.IsZero() {
		return nil, ErrInvalidEmail
	}
	if external.IsZero() {
		return nil, ErrInvalidExternalIdentity
	}
	canonicalName, err := normalizeName(name)
	if err != nil {
		return nil, err
	}

	registered := &User{
		id:           NewUserID(),
		email:        email,
		name:         canonicalName,
		avatar:       avatar, // pode ser zero value: sem avatar é válido
		external:     external,
		registeredAt: time.Now(),
	}
	registered.RecordEvent(NewRegistered(registered.id, email, external))
	return registered, nil
}

// normalizeName é a única casa da regra "o que é um nome válido": valida e
// devolve a forma canônica.
func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", ErrInvalidName
	}
	return name, nil
}

// ID retorna a identidade do usuário.
func (u *User) ID() UserID {
	return u.id
}

// Email retorna o e-mail do usuário.
func (u *User) Email() Email {
	return u.email
}

// Name retorna o nome do usuário.
func (u *User) Name() string {
	return u.name
}

// Avatar retorna a foto de perfil, ou o zero value se o usuário não tem uma.
// Sem comma-ok: o VO já expressa a ausência com IsZero, e devolver
// (AvatarURL, bool) daria dois jeitos de dizer a mesma coisa.
func (u *User) Avatar() AvatarURL {
	return u.avatar
}

// ExternalIdentity retorna a referência no provedor de identidade.
func (u *User) ExternalIdentity() ExternalIdentity {
	return u.external
}

// RegisteredAt retorna quando o usuário se registrou.
func (u *User) RegisteredAt() time.Time {
	return u.registeredAt
}

// SyncProfile atualiza e-mail e nome com o que o provedor informou no login.
//
// Existe porque o perfil vive FORA: a pessoa troca o e-mail ou o nome na
// conta Google e espera ver isso refletido aqui. Devolve `changed` para o
// use case decidir se precisa persistir — gravar em todo login seria uma
// escrita por request sem fato novo nenhum.
//
// A identidade externa NÃO é sincronizável: o subject é a âncora do
// usuário, e permitir trocá-lo seria permitir assumir a conta de outro.
func (u *User) SyncProfile(email Email, name string, avatar AvatarURL) (bool, error) {
	if email.IsZero() {
		return false, ErrInvalidEmail
	}
	canonicalName, err := normalizeName(name)
	if err != nil {
		return false, err
	}

	if u.email.Equals(email) && u.name == canonicalName && u.avatar.Equals(avatar) {
		return false, nil
	}

	u.email = email
	u.name = canonicalName
	// Avatar zerado SOBRESCREVE o anterior: se a pessoa removeu a foto no
	// Google, o certo é a nossa cópia refletir isso. Preservar o valor antigo
	// deixaria o sistema mostrando uma foto que o usuário decidiu apagar.
	u.avatar = avatar
	return true, nil
}

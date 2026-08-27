// Package entrepo implementa a porta identity.Repository sobre o client
// tipado gerado pelo Ent para este bounded context.
//
// Mesmas disciplinas do adapter do Financial Tracking: as entities do Ent
// são modelo de persistência (o aggregate vive em domain/), o Snapshot é a
// ponte, e erro de ORM não vaza — ent.IsNotFound vira o sentinel do
// aggregate.
package entrepo

import (
	"context"
	"database/sql"
	"fmt"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/entrepo/ent"
	entuser "github.com/luigimenezes13/financial-manager/internal/identity/adapter/entrepo/ent/user"
	identity "github.com/luigimenezes13/financial-manager/internal/identity/domain"
)

// NewClient monta o client do Ent deste BC sobre um *sql.DB já configurado.
//
// Recebe o MESMO *sql.DB dos outros bounded contexts: pool de conexão é
// recurso de processo, e abrir um por BC multiplicaria conexões sem
// necessidade. O que é separado por BC é o SCHEMA e o histórico de
// migrations, não a conexão.
func NewClient(database *sql.DB) *ent.Client {
	driver := entsql.OpenDB(dialect.Postgres, database)
	return ent.NewClient(ent.Driver(driver))
}

// UserRepository implementa identity.Repository.
type UserRepository struct {
	client *ent.Client
}

// Checagem de contrato em tempo de compilação.
var _ identity.Repository = (*UserRepository)(nil)

// NewUserRepository injeta o client por construtor.
func NewUserRepository(client *ent.Client) *UserRepository {
	return &UserRepository{client: client}
}

// Save persiste o usuário (upsert por id). UpdateNewValues ignora os campos
// Immutable do schema — id, a âncora (provedor + subject), registered_at e
// created_at. Só perfil e updated_at mudam num login seguinte, que é
// exatamente o que SyncProfile permite mudar.
func (r *UserRepository) Save(ctx context.Context, user *identity.User) error {
	snapshot := user.Snapshot()

	err := r.client.User.Create().
		SetID(snapshot.ID).
		SetEmail(snapshot.Email).
		SetName(snapshot.Name).
		SetAvatarURL(snapshot.AvatarURL).
		SetExternalProvider(snapshot.ExternalProvider).
		SetExternalSubject(snapshot.ExternalSubject).
		SetRegisteredAt(snapshot.RegisteredAt).
		OnConflictColumns(entuser.FieldID).
		UpdateNewValues().
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("entrepo: falha salvando usuário: %w", err)
	}
	return nil
}

// FindByID hidrata o usuário pelo ID. Ausência → identity.ErrNotFound.
func (r *UserRepository) FindByID(ctx context.Context, id identity.UserID) (*identity.User, error) {
	row, err := r.client.User.Get(ctx, id.UUID())
	if ent.IsNotFound(err) {
		return nil, identity.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("entrepo: falha lendo usuário: %w", err)
	}

	return identity.FromSnapshot(toUserSnapshot(row))
}

// FindByExternalIdentity hidrata o usuário pela referência no provedor — é a
// consulta do login, servida pelo índice único (provider, subject).
//
// Ausência é o caminho ESPERADO do primeiro acesso, e por isso vira
// ErrNotFound limpo em vez de erro embrulhado: o use case decide por
// errors.Is que é hora de provisionar.
func (r *UserRepository) FindByExternalIdentity(ctx context.Context, external identity.ExternalIdentity) (*identity.User, error) {
	row, err := r.client.User.Query().
		Where(
			entuser.ExternalProviderEQ(external.Provider()),
			entuser.ExternalSubjectEQ(external.Subject()),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, identity.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("entrepo: falha lendo usuário por identidade externa: %w", err)
	}

	return identity.FromSnapshot(toUserSnapshot(row))
}

// toUserSnapshot traduz entity → Snapshot.
func toUserSnapshot(row *ent.User) identity.UserSnapshot {
	return identity.UserSnapshot{
		ID:               row.ID,
		Email:            row.Email,
		Name:             row.Name,
		AvatarURL:        row.AvatarURL,
		ExternalProvider: row.ExternalProvider,
		ExternalSubject:  row.ExternalSubject,
		RegisteredAt:     row.RegisteredAt,
	}
}

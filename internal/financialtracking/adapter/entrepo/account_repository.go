package entrepo

import (
	"context"
	"fmt"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo/ent"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo/ent/account"
	domainaccount "github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

// AccountRepository implementa account.Repository.
//
// O alias `domainaccount` existe porque o package gerado pelo Ent também se
// chama `account` — e a ordem do alias é deliberada: o nome curto fica com o
// GERADO (usado só em constantes de coluna) e o domínio ganha o nome
// explícito, pra ninguém confundir entity de persistência com aggregate.
type AccountRepository struct {
	client *ent.Client
}

// Checagem de contrato em tempo de compilação: se a porta mudar, o build
// quebra aqui e não em runtime.
var _ domainaccount.Repository = (*AccountRepository)(nil)

// NewAccountRepository injeta o client por construtor.
func NewAccountRepository(client *ent.Client) *AccountRepository {
	return &AccountRepository{client: client}
}

// Save persiste a conta. A porta expõe UM Save, então quem decide entre
// insert e update é o OnConflict — o use case não precisa saber se a conta é
// nova. UpdateNewValues ignora id, user_id e created_at (campos Immutable no
// schema), exatamente o que se espera de um upsert de aggregate.
func (r *AccountRepository) Save(ctx context.Context, target *domainaccount.Account) error {
	snapshot := target.Snapshot()

	err := r.client.Account.Create().
		SetID(snapshot.ID).
		SetUserID(snapshot.UserID).
		SetName(snapshot.Name).
		SetKind(snapshot.Kind).
		SetBalanceAmount(snapshot.BalanceAmount).
		SetBalanceCurrency(snapshot.BalanceCurrency).
		SetBalanceAsOf(snapshot.BalanceAsOf).
		SetSourceProvider(snapshot.SourceProvider).
		SetSourceProviderAccountID(snapshot.SourceProviderAccountID).
		OnConflictColumns(account.FieldID).
		UpdateNewValues().
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("entrepo: falha salvando conta: %w", err)
	}
	return nil
}

// FindByID hidrata a conta pelo ID. Ausência → account.ErrNotFound.
func (r *AccountRepository) FindByID(ctx context.Context, id domainaccount.AccountID) (*domainaccount.Account, error) {
	row, err := r.client.Account.Get(ctx, id.UUID())
	if ent.IsNotFound(err) {
		return nil, domainaccount.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("entrepo: falha lendo conta: %w", err)
	}

	return domainaccount.FromSnapshot(toAccountSnapshot(row))
}

// toAccountSnapshot é o mapper de LEITURA: entity do Ent → Snapshot do
// aggregate. Função separada e sem receiver porque é tradução pura — dá pra
// testar sem banco.
func toAccountSnapshot(row *ent.Account) domainaccount.AccountSnapshot {
	return domainaccount.AccountSnapshot{
		ID:                      row.ID,
		UserID:                  row.UserID,
		Name:                    row.Name,
		Kind:                    row.Kind,
		BalanceAmount:           row.BalanceAmount,
		BalanceCurrency:         row.BalanceCurrency,
		BalanceAsOf:             row.BalanceAsOf,
		SourceProvider:          row.SourceProvider,
		SourceProviderAccountID: row.SourceProviderAccountID,
	}
}

// ListByUser devolve as contas do usuário, ordenadas por nome.
//
// A ordenação é por NOME e não por created_at: a lista existe pra pessoa
// achar a conta dela, e "Nubank" antes de "Santander" é previsível — ordem
// de criação obrigaria a caçar visualmente.
func (r *AccountRepository) ListByUser(ctx context.Context, userID shared.UserID) ([]*domainaccount.Account, error) {
	rows, err := r.client.Account.Query().
		Where(account.UserIDEQ(userID.UUID())).
		Order(ent.Asc(account.FieldName)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("entrepo: falha listando contas: %w", err)
	}

	accounts := make([]*domainaccount.Account, 0, len(rows))
	for _, row := range rows {
		// Cada linha volta pelo MESMO FromSnapshot da leitura unitária: uma
		// segunda montagem "mais rápida" para listagem seria um segundo
		// caminho com critérios próprios, que é como as duas divergem.
		rebuilt, err := domainaccount.FromSnapshot(toAccountSnapshot(row))
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, rebuilt)
	}

	return accounts, nil
}

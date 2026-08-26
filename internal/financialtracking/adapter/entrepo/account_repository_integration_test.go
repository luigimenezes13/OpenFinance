//go:build integration

package entrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
)

func mustUserID(t *testing.T, raw uuid.UUID) shared.UserID {
	t.Helper()
	userID, err := shared.NewUserID(raw)
	require.NoError(t, err)
	return userID
}

func mustCurrency(t *testing.T, code string) shared.Currency {
	t.Helper()
	currency, err := shared.NewCurrency(code)
	require.NoError(t, err)
	return currency
}

func mustMoneyBRL(t *testing.T, amount int64) shared.Money {
	t.Helper()
	money, err := shared.NewMoney(amount, mustCurrency(t, "BRL"))
	require.NoError(t, err)
	return money
}

func mustManualAccount(t *testing.T, ownerID uuid.UUID) *account.Account {
	t.Helper()
	created, err := account.New(mustUserID(t, ownerID), "Conta Corrente", account.KindChecking, mustCurrency(t, "BRL"), account.NewManualSource())
	require.NoError(t, err)
	return created
}

// TestIntegrationAccountRepositorySaveAndFind é o round-trip completo:
// aggregate → Ent → Postgres → Ent → aggregate.
func TestIntegrationAccountRepositorySaveAndFind(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewAccountRepository(testClient)
	original := mustManualAccount(t, uuid.New())

	require.NoError(t, repository.Save(ctx, original))

	found, err := repository.FindByID(ctx, original.ID())
	require.NoError(t, err)

	originalSnapshot := original.Snapshot()
	foundSnapshot := found.Snapshot()

	assert.Equal(t, originalSnapshot.ID, foundSnapshot.ID)
	assert.Equal(t, originalSnapshot.UserID, foundSnapshot.UserID)
	assert.Equal(t, originalSnapshot.Name, foundSnapshot.Name)
	assert.Equal(t, originalSnapshot.Kind, foundSnapshot.Kind)
	assert.Equal(t, originalSnapshot.BalanceAmount, foundSnapshot.BalanceAmount)
	assert.Equal(t, originalSnapshot.BalanceCurrency, foundSnapshot.BalanceCurrency)
	assert.Empty(t, foundSnapshot.SourceProvider, "conta manual não carrega referência de provider")

	// O instante NÃO volta bit a bit: timestamptz tem precisão de
	// MICROssegundo e o time.Time do Go tem nanossegundo — o banco trunca. O
	// Ent não muda isso; é o Postgres. Pegadinha nº 1 de round-trip de data.
	assert.WithinDuration(t, originalSnapshot.BalanceAsOf, foundSnapshot.BalanceAsOf, time.Millisecond)
}

// TestIntegrationAccountRepositorySaveIsUpsert prova que o OnConflict do Ent
// se comporta como upsert de aggregate: atualiza a linha, não duplica, e não
// mexe nos campos Immutable.
func TestIntegrationAccountRepositorySaveIsUpsert(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewAccountRepository(testClient)
	target := mustManualAccount(t, uuid.New())
	require.NoError(t, repository.Save(ctx, target))

	var createdAtBefore time.Time
	targetID := target.ID()
	require.NoError(t, testDB.QueryRowContext(ctx,
		`SELECT created_at FROM accounts WHERE id = $1`, targetID.UUID()).Scan(&createdAtBefore))

	newBalance, err := account.NewBalance(mustMoneyBRL(t, -1_250_00), time.Now())
	require.NoError(t, err)
	require.NoError(t, target.UpdateBalance(newBalance))
	require.NoError(t, target.Rename("Conta Corrente Nubank"))
	require.NoError(t, repository.Save(ctx, target))

	found, err := repository.FindByID(ctx, target.ID())
	require.NoError(t, err)

	foundSnapshot := found.Snapshot()
	assert.Equal(t, int64(-1_250_00), foundSnapshot.BalanceAmount)
	assert.Equal(t, "Conta Corrente Nubank", foundSnapshot.Name)

	var rowCount int
	require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM accounts`).Scan(&rowCount))
	assert.Equal(t, 1, rowCount, "upsert atualiza a linha, não insere outra")

	// created_at é Immutable no schema, então o UpdateNewValues do Ent o
	// ignora no conflito — updated_at, não.
	var createdAtAfter, updatedAtAfter time.Time
	require.NoError(t, testDB.QueryRowContext(ctx,
		`SELECT created_at, updated_at FROM accounts WHERE id = $1`, targetID.UUID()).
		Scan(&createdAtAfter, &updatedAtAfter))
	assert.WithinDuration(t, createdAtBefore, createdAtAfter, time.Microsecond, "created_at não muda no update")
	assert.True(t, updatedAtAfter.After(createdAtAfter) || updatedAtAfter.Equal(createdAtAfter), "updated_at acompanha a escrita")

	// O evento acumulado pelo UpdateBalance continua no aggregate: o
	// repositório NÃO despacha nem limpa evento — isso é papel do use case.
	assert.Len(t, target.Events(), 1)
}

// TestIntegrationAccountRepositoryConnectedAccount cobre o outro caminho do
// VO Source e o índice único parcial gerado do schema.
func TestIntegrationAccountRepositoryConnectedAccount(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewAccountRepository(testClient)
	source, err := account.NewOpenFinanceSource("pluggy", "pluggy-acc-77")
	require.NoError(t, err)
	connected, err := account.New(mustUserID(t, uuid.New()), "Conta Conectada", account.KindCreditCard, mustCurrency(t, "BRL"), source)
	require.NoError(t, err)

	require.NoError(t, repository.Save(ctx, connected))

	found, err := repository.FindByID(ctx, connected.ID())
	require.NoError(t, err)

	foundSource := found.Source()
	require.True(t, foundSource.IsOpenFinance())
	provider, ok := foundSource.Provider()
	require.True(t, ok)
	assert.Equal(t, "pluggy", provider)
	providerAccountID, ok := foundSource.ProviderAccountID()
	require.True(t, ok)
	assert.Equal(t, "pluggy-acc-77", providerAccountID)

	duplicate, err := account.New(mustUserID(t, uuid.New()), "Duplicada", account.KindChecking, mustCurrency(t, "BRL"), source)
	require.NoError(t, err)
	require.Error(t, repository.Save(ctx, duplicate),
		"duas contas locais pra mesma conta do provider tem que falhar")
}

// TestIntegrationAccountManualAccountsIgnoreProviderIndex: duas contas
// MANUAIS coexistem, porque o índice é PARCIAL (WHERE source_provider <> ”).
// Sem o WHERE no schema, a segunda conta manual do sistema quebraria.
func TestIntegrationAccountManualAccountsIgnoreProviderIndex(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewAccountRepository(testClient)
	ownerID := uuid.New()

	require.NoError(t, repository.Save(ctx, mustManualAccount(t, ownerID)))
	require.NoError(t, repository.Save(ctx, mustManualAccount(t, ownerID)))

	var rowCount int
	require.NoError(t, testDB.QueryRowContext(ctx, `SELECT count(*) FROM accounts`).Scan(&rowCount))
	assert.Equal(t, 2, rowCount)
}

// TestIntegrationAccountRepositoryNotFound: o ent.IsNotFound é traduzido pro
// sentinel do aggregate — erro do ORM não vaza pro domínio.
func TestIntegrationAccountRepositoryNotFound(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	repository := entrepo.NewAccountRepository(testClient)
	missingID, err := account.AccountIDFromUUID(uuid.New())
	require.NoError(t, err)

	found, err := repository.FindByID(ctx, missingID)

	require.ErrorIs(t, err, account.ErrNotFound)
	assert.Nil(t, found)
}

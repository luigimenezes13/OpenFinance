package application

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/openfinance"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
)

// ImportFromProviderInput é a entrada da importação. A conta é a LOCAL (a
// referência do provider sai do VO Source dela — o cliente não manda id de
// provider, senão qualquer um importaria pra conta de qualquer um).
// Since zero = todo o histórico disponível.
type ImportFromProviderInput struct {
	UserID    uuid.UUID
	AccountID uuid.UUID
	Since     time.Time
}

// ImportFromProviderOutput é a projeção do resultado: quais transações
// nasceram nesta importação. Lista vazia é resultado legítimo (nada novo no
// provider), não erro.
type ImportFromProviderOutput struct {
	AccountID      string
	Provider       string
	TransactionIDs []string
}

// ImportFromProviderUseCase importa transações de uma conta conectada.
// Quatro dependências: transações (grava), contas (lê — ownership, moeda e
// referência do provider), a porta Provider (lê o mundo externo) e o
// Dispatcher (NewFromProvider emite Imported).
//
// Este é o use case que materializa a ACL: ele recebe ProviderTransaction
// (DTO de fronteira) e produz aggregate Transaction. Nada de Pluggy entra
// aqui — o adapter já traduziu.
type ImportFromProviderUseCase struct {
	transactions transaction.Repository
	accounts     account.Repository
	provider     openfinance.Provider
	dispatcher   events.Dispatcher
}

// NewImportFromProviderUseCase injeta as dependências por construtor.
func NewImportFromProviderUseCase(
	transactions transaction.Repository,
	accounts account.Repository,
	provider openfinance.Provider,
	dispatcher events.Dispatcher,
) *ImportFromProviderUseCase {
	return &ImportFromProviderUseCase{
		transactions: transactions,
		accounts:     accounts,
		provider:     provider,
		dispatcher:   dispatcher,
	}
}

// Execute orquestra a importação: resolve a conta local, descobre a
// contraparte no provider, busca as transações e transforma cada uma em
// aggregate.
//
// LIMITE CONHECIDO do v1 (spec: idempotência é decisão do PR4): rodar a
// importação duas vezes na mesma janela IMPORTA DE NOVO. Deduplicar exige
// consultar por ExternalRef — método que a porta transaction.Repository
// ainda não tem, e que a doc §5.5 mostra ser mais complicado que "compara o
// id" (o id da Pluggy muda quando a transação é reconciliada). Fica
// explícito aqui em vez de escondido num TODO.
func (u *ImportFromProviderUseCase) Execute(ctx context.Context, input ImportFromProviderInput) (ImportFromProviderOutput, error) {
	userID, err := shared.NewUserID(input.UserID)
	if err != nil {
		return ImportFromProviderOutput{}, err
	}

	accountID, err := account.AccountIDFromUUID(input.AccountID)
	if err != nil {
		return ImportFromProviderOutput{}, err
	}

	targetAccount, err := u.accounts.FindByID(ctx, accountID)
	if err != nil {
		return ImportFromProviderOutput{}, err
	}
	if err := assertOwnership(targetAccount.UserID(), userID); err != nil {
		return ImportFromProviderOutput{}, err
	}

	connection, err := u.resolveConnection(targetAccount)
	if err != nil {
		return ImportFromProviderOutput{}, err
	}

	providerTransactions, err := u.provider.FetchTransactions(ctx, connection.providerAccountID, input.Since)
	if err != nil {
		return ImportFromProviderOutput{}, err
	}

	balance := targetAccount.Balance()
	currency := balance.Currency()

	transactionIDs := make([]string, 0, len(providerTransactions))
	for _, providerTransaction := range providerTransactions {
		imported, err := u.importOne(ctx, userID, accountID, currency, connection.provider, providerTransaction)
		if err != nil {
			return ImportFromProviderOutput{}, err
		}
		transactionIDs = append(transactionIDs, imported.ID().String())
	}

	return ImportFromProviderOutput{
		AccountID:      accountID.String(),
		Provider:       connection.provider,
		TransactionIDs: transactionIDs,
	}, nil
}

// connection é o par (provider, id da conta lá) extraído do VO Source.
// Struct local só pra não devolver dois strings soltos de resolveConnection
// — a ordem de dois retornos do mesmo tipo é fácil de trocar sem o
// compilador reclamar.
type connection struct {
	provider          string
	providerAccountID string
}

// resolveConnection descobre a contraparte da conta no provider e recusa os
// dois casos impossíveis: conta manual (não tem o que importar) e conta de
// OUTRO provider (adapter da Belvo não fala com conta da Pluggy).
func (u *ImportFromProviderUseCase) resolveConnection(targetAccount *account.Account) (connection, error) {
	source := targetAccount.Source()
	if !source.IsOpenFinance() {
		return connection{}, openfinance.ErrAccountNotConnected
	}

	// Comma-ok: IsOpenFinance acima já garante que os dois campos existem,
	// então o segundo retorno é descartado — o VO é que protege a
	// invariante "openfinance tem ref, manual não tem".
	providerName, _ := source.Provider()
	providerAccountID, _ := source.ProviderAccountID()

	if providerName != u.provider.Name() {
		return connection{}, openfinance.ErrProviderMismatch
	}

	return connection{provider: providerName, providerAccountID: providerAccountID}, nil
}

// importOne transforma UM ProviderTransaction em aggregate persistido, com
// os eventos já publicados. Método separado pra manter um nível de
// indentação no laço de Execute — e porque "importar uma transação" é um
// passo com nome próprio.
//
// Falha aqui aborta a importação inteira (v1): mais simples e determinístico
// que importar metade. Tolerância por item (importa o que dá, relata o que
// falhou) é decisão do PR4, quando existir sync recorrente.
func (u *ImportFromProviderUseCase) importOne(
	ctx context.Context,
	userID shared.UserID,
	accountID account.AccountID,
	currency shared.Currency,
	providerName string,
	providerTransaction openfinance.ProviderTransaction,
) (*transaction.Transaction, error) {
	// Moeda divergente é dado corrompido na fronteira, não escolha do
	// usuário: conta em BRL não recebe transação em USD.
	if providerTransaction.CurrencyCode != currency.Code() {
		return nil, account.ErrCurrencyMismatch
	}

	money, err := shared.NewMoney(providerTransaction.AmountInCents, currency)
	if err != nil {
		return nil, err
	}

	ref, err := transaction.NewExternalRef(providerName, providerTransaction.ProviderTransactionID)
	if err != nil {
		return nil, err
	}

	imported, err := transaction.NewFromProvider(
		userID,
		accountID,
		money,
		providerTransaction.OccurredAt,
		providerTransaction.Description,
		ref,
	)
	if err != nil {
		return nil, err
	}

	if err := u.transactions.Save(ctx, imported); err != nil {
		return nil, err
	}

	if err := dispatchAndClear(ctx, u.dispatcher, imported); err != nil {
		return nil, err
	}

	return imported, nil
}

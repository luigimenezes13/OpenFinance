package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/openfinance"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/shared"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
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
// nasceram nesta importação e o saldo que veio do provedor. Lista vazia é
// resultado legítimo (nada novo no provider), não erro.
type ImportFromProviderOutput struct {
	AccountID      string
	Provider       string
	TransactionIDs []string

	// Balance é o saldo informado pelo provedor, em centavos, e Currency a
	// moeda dele. É o ÚNICO caminho pelo qual o saldo de uma conta muda no
	// sistema (decisão de 2026-08-27).
	Balance  int64
	Currency string

	// BalanceApplied é false quando o provedor devolveu um saldo mais ANTIGO
	// que o já registrado e ele foi ignorado. Sai no output em vez de virar
	// erro porque não é falha: é sync fora de ordem, que acontece.
	BalanceApplied bool
}

// ImportFromProviderUseCase importa transações de uma conta conectada.
// Quatro dependências: transações (grava), contas (lê e grava o saldo), a
// porta Provider (lê o mundo externo) e o UnitOfWork, que torna a gravação
// das N transações e do saldo uma coisa só.
//
// Este é o use case que materializa a ACL: ele recebe ProviderTransaction
// (DTO de fronteira) e produz aggregate Transaction. Nada de Pluggy entra
// aqui — o adapter já traduziu.
type ImportFromProviderUseCase struct {
	transactions transaction.Repository
	accounts     account.Repository
	provider     openfinance.Provider
	unitOfWork   kernel.UnitOfWork
}

func NewImportFromProviderUseCase(
	transactions transaction.Repository,
	accounts account.Repository,
	provider openfinance.Provider,
	unitOfWork kernel.UnitOfWork,
) *ImportFromProviderUseCase {
	return &ImportFromProviderUseCase{
		transactions: transactions,
		accounts:     accounts,
		provider:     provider,
		unitOfWork:   unitOfWork,
	}
}

// Execute busca tudo no provider ANTES de abrir a transação do banco (rede
// dentro de transação seguraria lock pelo tempo da chamada externa) e depois
// grava as transações importadas e o saldo numa unidade só: falha no meio
// não deixa metade da importação salva.
//
// LIMITE CONHECIDO do v1 (spec: idempotência é decisão do PR4): rodar a
// importação duas vezes na mesma janela IMPORTA DE NOVO. Deduplicar exige
// consultar por ExternalRef — método que a porta transaction.Repository
// ainda não tem, e que a doc §5.5 mostra ser mais complicado que "compara o
// id" (o id da Pluggy muda quando a transação é reconciliada).
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
	providerBalance, err := u.provider.FetchBalance(ctx, connection.providerAccountID)
	if err != nil {
		return ImportFromProviderOutput{}, err
	}

	balance := targetAccount.Balance()
	currency := balance.Currency()

	imported, err := toImportedTransactions(userID, accountID, currency, connection.provider, providerTransactions)
	if err != nil {
		return ImportFromProviderOutput{}, err
	}
	applied, err := applyProviderBalance(targetAccount, currency, providerBalance)
	if err != nil {
		return ImportFromProviderOutput{}, err
	}

	err = u.unitOfWork.Run(ctx, func(ctx context.Context) error {
		return u.persist(ctx, imported, targetAccount, applied)
	})
	if err != nil {
		return ImportFromProviderOutput{}, err
	}

	return ImportFromProviderOutput{
		AccountID:      accountID.String(),
		Provider:       connection.provider,
		TransactionIDs: transactionIDsOf(imported),
		Balance:        providerBalance.AmountInCents,
		Currency:       providerBalance.CurrencyCode,
		BalanceApplied: applied,
	}, nil
}

func (u *ImportFromProviderUseCase) persist(
	ctx context.Context,
	imported []*transaction.Transaction,
	targetAccount *account.Account,
	balanceApplied bool,
) error {
	for _, importedTransaction := range imported {
		if err := u.transactions.Save(ctx, importedTransaction); err != nil {
			return err
		}
	}
	if !balanceApplied {
		return nil
	}
	return u.accounts.Save(ctx, targetAccount)
}

func transactionIDsOf(imported []*transaction.Transaction) []string {
	ids := make([]string, 0, len(imported))
	for _, importedTransaction := range imported {
		ids = append(ids, importedTransaction.ID().String())
	}
	return ids
}

// applyProviderBalance registra na conta o saldo informado pelo provedor.
//
// Este é o ÚNICO lugar do sistema que muda saldo de conta — nem o lançamento
// manual, nem a categorização mexem nele (decisão de 2026-08-27): o banco é a
// autoridade sobre quanto existe na conta, e derivar saldo dos lançamentos
// digitados produziria um número que discorda do extrato bancário.
//
// Saldo mais antigo que o registrado é IGNORADO, não é erro: webhooks e syncs
// chegam fora de ordem, e o aggregate recusa o retrocesso (ErrStaleBalance,
// decisão de 2026-07-04). O retorno false diz que a conta não mudou.
func applyProviderBalance(
	targetAccount *account.Account,
	currency shared.Currency,
	providerBalance openfinance.ProviderBalance,
) (bool, error) {
	if providerBalance.CurrencyCode != currency.Code() {
		return false, account.ErrCurrencyMismatch
	}

	money, err := shared.NewMoney(providerBalance.AmountInCents, currency)
	if err != nil {
		return false, err
	}
	updated, err := account.NewBalance(money, providerBalance.AsOf)
	if err != nil {
		return false, err
	}

	err = targetAccount.UpdateBalance(updated)
	if errors.Is(err, account.ErrStaleBalance) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
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

// toImportedTransactions transforma cada ProviderTransaction em aggregate,
// sem gravar nada. Um item inválido aborta a importação inteira (v1): mais
// simples e determinístico que importar metade. Tolerância por item é decisão
// do PR4, quando existir sync recorrente.
func toImportedTransactions(
	userID shared.UserID,
	accountID account.AccountID,
	currency shared.Currency,
	providerName string,
	providerTransactions []openfinance.ProviderTransaction,
) ([]*transaction.Transaction, error) {
	imported := make([]*transaction.Transaction, 0, len(providerTransactions))
	for _, providerTransaction := range providerTransactions {
		converted, err := toImportedTransaction(userID, accountID, currency, providerName, providerTransaction)
		if err != nil {
			return nil, err
		}
		imported = append(imported, converted)
	}
	return imported, nil
}

// toImportedTransaction recusa moeda divergente: é dado corrompido na
// fronteira, não escolha do usuário — conta em BRL não recebe transação em USD.
func toImportedTransaction(
	userID shared.UserID,
	accountID account.AccountID,
	currency shared.Currency,
	providerName string,
	providerTransaction openfinance.ProviderTransaction,
) (*transaction.Transaction, error) {
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

	return transaction.NewFromProvider(
		userID,
		accountID,
		money,
		providerTransaction.OccurredAt,
		providerTransaction.Description,
		ref,
	)
}

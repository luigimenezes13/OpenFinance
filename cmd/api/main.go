// Command api é o COMPOSITION ROOT do serviço: o único lugar do sistema que
// conhece tipos concretos de infraestrutura.
//
// É aqui que a inversão de dependência se paga de verdade. Trocar Postgres
// por outro banco, o dispatcher in-process por outbox, ou o mock de Open
// Finance pela Pluggy é trocar a linha que INSTANCIA — nenhum use case,
// nenhum aggregate, nenhum handler muda. A prova prática: a persistência já
// mudou de pgx cru pra Ent sem tocar em domain/ nem application/.
//
// Injeção manual, sem framework de DI: o grafo é pequeno, e ler o
// construtor em ordem é mais claro que ler anotações e descobrir em runtime
// o que faltou.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/ginhandler"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/openfinance/mockprovider"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/account"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/domain/transaction"
	identityentrepo "github.com/luigimenezes13/financial-manager/internal/identity/adapter/entrepo"
	identityginhandler "github.com/luigimenezes13/financial-manager/internal/identity/adapter/ginhandler"
	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/ginmiddleware"
	identityapplication "github.com/luigimenezes13/financial-manager/internal/identity/application"
	identitydomain "github.com/luigimenezes13/financial-manager/internal/identity/domain"
	"github.com/luigimenezes13/financial-manager/internal/kernel/events"
	"github.com/luigimenezes13/financial-manager/internal/platform/config"
	"github.com/luigimenezes13/financial-manager/internal/platform/db"
	platformevents "github.com/luigimenezes13/financial-manager/internal/platform/events"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// run devolve erro em vez de chamar os.Exit no meio: assim os defers
	// (fechar pool, fechar client) realmente rodam. os.Exit dentro de run
	// pularia todos eles.
	if err := run(logger); err != nil {
		logger.Error("falha subindo o serviço", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	// Sinal do sistema vira cancelamento de context: é o que permite
	// shutdown gracioso em vez de matar requests no meio.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	configuration, err := config.Load()
	if err != nil {
		return err
	}

	database, err := db.NewSQLDB(ctx, configuration.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := database.Close(); closeErr != nil {
			logger.Error("falha fechando conexão", slog.String("error", closeErr.Error()))
		}
	}()

	// Um client Ent por bounded context, sobre o MESMO pool: schema e
	// histórico de migrations são por BC; conexão é recurso de processo.
	entClient := entrepo.NewClient(database)
	defer func() {
		if closeErr := entClient.Close(); closeErr != nil {
			logger.Error("falha fechando client", slog.String("error", closeErr.Error()))
		}
	}()

	identityClient := identityentrepo.NewClient(database)
	defer func() {
		if closeErr := identityClient.Close(); closeErr != nil {
			logger.Error("falha fechando client de identidade", slog.String("error", closeErr.Error()))
		}
	}()

	// --- Adapters concretos (a única parte do sistema que sabe o "quem") ---
	accounts := entrepo.NewAccountRepository(entClient)
	transactions := entrepo.NewTransactionRepository(entClient)
	categories := entrepo.NewCategoryRepository(entClient)
	provider := mockprovider.New()
	dispatcher := platformevents.NewInProcessDispatcher(logger)

	users := identityentrepo.NewUserRepository(identityClient)
	// Qual verificador entra é decidido em tempo de COMPILAÇÃO (ver
	// verifier_google.go e verifier_devauth.go): o binário de produção não
	// contém o de desenvolvimento.
	verifier, err := newTokenVerifier(configuration, logger)
	if err != nil {
		return err
	}

	registerEventLogging(dispatcher, logger)

	// --- Use cases (só conhecem as portas) ---
	signIn := identityapplication.NewSignInUseCase(users, verifier, dispatcher)
	viewProfile := identityapplication.NewViewProfileUseCase(users)

	createAccount := application.NewCreateAccountUseCase(accounts)
	listAccounts := application.NewListAccountsUseCase(accounts)
	viewAccount := application.NewViewAccountUseCase(accounts)
	createCategory := application.NewCreateCategoryUseCase(categories)
	listCategories := application.NewListCategoriesUseCase(categories)
	listTransactions := application.NewListTransactionsUseCase(transactions)
	viewTransaction := application.NewViewTransactionUseCase(transactions)
	recordTransaction := application.NewRecordTransactionUseCase(transactions, accounts)
	categorizeTransaction := application.NewCategorizeTransactionUseCase(transactions, categories, dispatcher)
	importFromProvider := application.NewImportFromProviderUseCase(transactions, accounts, provider, dispatcher)

	// --- Borda HTTP ---
	router := gin.New()
	// Recovery evita que panic em um handler derrube o processo inteiro.
	router.Use(gin.Recovery())

	// O middleware do BC Identity é montado aqui e entregue pronto ao BC
	// Financial Tracking: as rotas exigem "usuário resolvido", sem saber
	// como.
	authenticate := ginmiddleware.Authenticate(ginmiddleware.UserResolverFrom(signIn))

	ginhandler.RegisterRoutes(
		router,
		database,
		authenticate,
		ginhandler.NewAccountHandler(createAccount, listAccounts, viewAccount, logger),
		ginhandler.NewTransactionHandler(
			recordTransaction, categorizeTransaction, importFromProvider,
			listTransactions, viewTransaction, logger,
		),
		ginhandler.NewCategoryHandler(createCategory, listCategories, logger),
	)

	// Cada bounded context registra as SUAS rotas com o mesmo middleware.
	identityginhandler.RegisterRoutes(
		router,
		authenticate,
		identityginhandler.NewProfileHandler(viewProfile, logger),
	)

	return serve(ctx, logger, configuration, router)
}

// serve sobe o servidor e espera pelo sinal de parada.
func serve(ctx context.Context, logger *slog.Logger, configuration config.Config, router http.Handler) error {
	server := &http.Server{Addr: configuration.HTTPAddr, Handler: router}

	// O servidor roda em goroutine própria porque ListenAndServe bloqueia
	// até o servidor morrer — e precisamos da main livre pra esperar o
	// sinal de shutdown.
	serverFailed := make(chan error, 1)
	go func() {
		logger.Info("servidor ouvindo", slog.String("addr", configuration.HTTPAddr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverFailed <- err
			return
		}
		serverFailed <- nil
	}()

	select {
	case err := <-serverFailed:
		return err
	case <-ctx.Done():
		logger.Info("sinal recebido, encerrando")
	}

	// Context NOVO pro shutdown: o antigo já está cancelado (foi o sinal
	// que o cancelou), e usá-lo abortaria as requests em voo na hora — o
	// oposto de shutdown gracioso.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), configuration.ShutdownTimeout)
	defer cancel()

	return server.Shutdown(shutdownCtx)
}

// registerEventLogging assina os eventos de domínio com um handler de LOG.
//
// No v1 nenhum consumidor reage de verdade (Budgets e Goals são PR5), mas
// registrar o log aqui serve a dois propósitos: prova que a assinatura do
// dispatcher funciona ponta a ponta, e dá rastro observável de que o
// aggregate emitiu o fato certo no momento certo.
func registerEventLogging(dispatcher *platformevents.InProcessDispatcher, logger *slog.Logger) {
	logEvent := func(ctx context.Context, event events.Event) error {
		logger.InfoContext(ctx, "evento de domínio",
			slog.String("event", event.EventName()),
			slog.Time("occurred_at", event.OccurredAt()),
		)
		return nil
	}

	dispatcher.Register(transaction.EventTypeImported, logEvent)
	dispatcher.Register(transaction.EventTypeCategorized, logEvent)
	dispatcher.Register(transaction.EventTypeReconciled, logEvent)
	dispatcher.Register(account.EventTypeBalanceUpdated, logEvent)
	dispatcher.Register(identitydomain.EventTypeRegistered, logEvent)
}

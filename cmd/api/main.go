// Command api é o COMPOSITION ROOT do serviço: o único lugar do sistema que
// conhece tipos concretos de infraestrutura.
//
// É aqui que a inversão de dependência se paga de verdade. Trocar Postgres
// por outro banco, o River por outra fila do outbox, ou o mock de Open
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
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"

	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/entrepo"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/ginhandler"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/adapter/openfinance/mockprovider"
	"github.com/luigimenezes13/financial-manager/internal/financialtracking/application"
	identityentrepo "github.com/luigimenezes13/financial-manager/internal/identity/adapter/entrepo"
	identityginhandler "github.com/luigimenezes13/financial-manager/internal/identity/adapter/ginhandler"
	"github.com/luigimenezes13/financial-manager/internal/identity/adapter/ginmiddleware"
	identityapplication "github.com/luigimenezes13/financial-manager/internal/identity/application"
	"github.com/luigimenezes13/financial-manager/internal/kernel"
	"github.com/luigimenezes13/financial-manager/internal/platform/config"
	"github.com/luigimenezes13/financial-manager/internal/platform/db"
	"github.com/luigimenezes13/financial-manager/internal/platform/httpmiddleware"
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
	unitOfWork := db.NewUnitOfWork(database)
	outboxRuntime, err := newOutbox(database, configuration, logger)
	if err != nil {
		return err
	}
	transactionRunner := kernel.NewTransactionRunner(unitOfWork, outboxRuntime.recorder)

	accounts := entrepo.NewAccountRepository(entClient)
	transactions := entrepo.NewTransactionRepository(entClient)
	categories := entrepo.NewCategoryRepository(entClient, unitOfWork)
	provider := mockprovider.New()

	users := identityentrepo.NewUserRepository(identityClient)
	// Qual verificador entra é decidido em tempo de COMPILAÇÃO (ver
	// verifier_google.go e verifier_devauth.go): o binário de produção não
	// contém o de desenvolvimento.
	verifier, err := newTokenVerifier(configuration, logger)
	if err != nil {
		return err
	}

	// --- Use cases (só conhecem as portas) ---
	signIn := identityapplication.NewSignInUseCase(users, verifier, transactionRunner)
	viewProfile := identityapplication.NewViewProfileUseCase(users)

	createAccount := application.NewCreateAccountUseCase(accounts, transactionRunner)
	listAccounts := application.NewListAccountsUseCase(accounts)
	viewAccount := application.NewViewAccountUseCase(accounts)
	renameAccount := application.NewRenameAccountUseCase(accounts, transactionRunner)
	createCategory := application.NewCreateCategoryUseCase(categories, transactionRunner)
	listCategories := application.NewListCategoriesUseCase(categories)
	renameCategory := application.NewRenameCategoryUseCase(categories, transactionRunner)
	moveCategory := application.NewMoveCategoryUseCase(categories, transactionRunner)
	addCategoryRule := application.NewAddCategoryRuleUseCase(categories, transactionRunner)
	removeCategoryRule := application.NewRemoveCategoryRuleUseCase(categories, transactionRunner)
	listTransactions := application.NewListTransactionsUseCase(transactions)
	viewTransaction := application.NewViewTransactionUseCase(transactions)
	recordTransaction := application.NewRecordTransactionUseCase(transactions, accounts, transactionRunner)
	categorizeTransaction := application.NewCategorizeTransactionUseCase(transactions, categories, transactionRunner)
	importFromProvider := application.NewImportFromProviderUseCase(transactions, accounts, provider, transactionRunner)

	// --- Borda HTTP ---
	router := gin.New()
	// Recovery evita que panic em um handler derrube o processo inteiro.
	router.Use(gin.Recovery())
	// CORS vem ANTES da autenticação: o preflight OPTIONS do browser não
	// carrega credencial, e se ele passasse pelo middleware de identidade
	// voltaria 401 — que o browser mostra como "CORS negado", escondendo a
	// causa.
	router.Use(httpmiddleware.CORS(configuration.CORSAllowedOrigins))

	// O middleware do BC Identity é montado aqui e entregue pronto ao BC
	// Financial Tracking: as rotas exigem "usuário resolvido", sem saber
	// como.
	authenticate := ginmiddleware.Authenticate(ginmiddleware.UserResolverFrom(signIn))

	ginhandler.RegisterRoutes(
		router,
		database,
		authenticate,
		ginhandler.NewAccountHandler(createAccount, listAccounts, viewAccount, renameAccount, logger),
		ginhandler.NewTransactionHandler(
			recordTransaction, categorizeTransaction, importFromProvider,
			listTransactions, viewTransaction, logger,
		),
		ginhandler.NewCategoryHandler(
			createCategory, listCategories, renameCategory, moveCategory,
			addCategoryRule, removeCategoryRule, logger,
		),
	)

	// Cada bounded context registra as SUAS rotas com o mesmo middleware.
	identityginhandler.RegisterRoutes(
		router,
		authenticate,
		identityginhandler.NewProfileHandler(viewProfile, logger),
	)

	return serve(ctx, logger, configuration, router, outboxRuntime)
}

// serve runs the HTTP server and the outbox worker side by side until the
// signal arrives or either of them fails, then stops both.
func serve(ctx context.Context, logger *slog.Logger, configuration config.Config, router http.Handler, outboxRuntime outboxRuntime) error {
	server := &http.Server{Addr: configuration.HTTPAddr, Handler: router}
	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		logger.Info("servidor ouvindo", slog.String("addr", configuration.HTTPAddr))
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	group.Go(func() error {
		return outboxRuntime.start(groupCtx)
	})
	group.Go(func() error {
		<-groupCtx.Done()
		logger.Info("encerrando")
		return shutdown(server, outboxRuntime, configuration.ShutdownTimeout)
	})

	return group.Wait()
}

// shutdown uses a fresh context because the signal already cancelled the
// original one, and reusing it would abort in-flight requests instead of
// draining them. The server drains first so no request records an event after
// the worker is gone.
func shutdown(server *http.Server, outboxRuntime outboxRuntime, grace time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	serverErr := server.Shutdown(shutdownCtx)
	return errors.Join(serverErr, outboxRuntime.stop(shutdownCtx, grace))
}

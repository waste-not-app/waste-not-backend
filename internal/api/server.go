package api

import (
	"log/slog"
	"net/http"
	"time"

	swaggo "github.com/gofiber/contrib/v3/swaggo"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/waste-not-app/waste-not-backend/docs"
	"github.com/waste-not-app/waste-not-backend/internal/ai"
	"github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/middleware"
	"github.com/waste-not-app/waste-not-backend/internal/modules/achievements"
	aiMod "github.com/waste-not-app/waste-not-backend/internal/modules/ai"
	"github.com/waste-not-app/waste-not-backend/internal/modules/analytics"
	authMod "github.com/waste-not-app/waste-not-backend/internal/modules/auth"
	"github.com/waste-not-app/waste-not-backend/internal/modules/guides"
	"github.com/waste-not-app/waste-not-backend/internal/modules/logs"
	"github.com/waste-not-app/waste-not-backend/internal/modules/photos"
	"github.com/waste-not-app/waste-not-backend/internal/modules/system"
	"github.com/waste-not-app/waste-not-backend/internal/shared/ratelimit"
	"github.com/waste-not-app/waste-not-backend/internal/shared/response"
)

type Server struct {
	App     *fiber.App
	DB      *pgxpool.Pool
	Config  config.Config
	Signer  auth.Signer
	AI      *ai.Client
	Limiter *ratelimit.Limiter
	Now     func() time.Time
	Log     *slog.Logger
}

func New(db *pgxpool.Pool, c config.Config, log *slog.Logger) (*Server, error) {
	aiClient, err := ai.New(c)
	if err != nil {
		return nil, err
	}
	dummyHash, err := auth.Hash(uuid.NewString())
	if err != nil {
		return nil, err
	}

	signer := auth.Signer{
		Secret:   c.Secret,
		Issuer:   c.Issuer,
		Audience: c.Audience,
	}
	limiter := ratelimit.NewLimiter(db, time.Now)
	cursorCodec := cursor.NewCodec(c.Secret)

	app := fiber.New(fiber.Config{
		AppName:      "WasteNot API v1",
		BodyLimit:    10 * 1024 * 1024,
		ErrorHandler: response.ErrorHandler(log),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 40 * time.Second,
		IdleTimeout:  60 * time.Second,
		ProxyHeader:  "X-Real-IP",
		TrustProxy:   true,
		TrustProxyConfig: fiber.TrustProxyConfig{
			Loopback: true,
		},
	})

	app.Use(middleware.RequestID(log))
	app.Use(recover.New())
	app.Use(compress.New())

	authMw := middleware.Protected(db, signer, limiter, log)

	var photoStorage photos.Storage
	if c.PhotoStorageDir != "" {
		photoStorage, err = photos.NewLocalStorage(c.PhotoStorageDir)
		if err != nil {
			return nil, err
		}
	}

	// Initialize repositories & services per module
	sysRepo := system.NewRepository(db)
	sysService := system.NewService(sysRepo, c, aiClient)

	authRepo := authMod.NewRepository(db)
	authService := authMod.NewService(authRepo, signer, limiter, dummyHash)

	logsRepo := logs.NewRepository(db, photoStorage)
	logsService := logs.NewService(logsRepo, c, limiter, cursorCodec)

	analyticsRepo := analytics.NewRepository(db)
	analyticsService := analytics.NewService(analyticsRepo, limiter)

	guidesRepo := guides.NewRepository(db)
	guidesService := guides.NewService(guidesRepo, cursorCodec)

	achievementsRepo := achievements.NewRepository(db)
	achievementsService := achievements.NewService(achievementsRepo, limiter)

	photosRepo := photos.NewRepository(db)
	photosService := photos.NewService(photosRepo, photoStorage)

	aiRepo := aiMod.NewRepository(db)
	aiService := aiMod.NewService(aiRepo, c, aiClient, limiter)

	// Swagger UI is opt-in (SWAGGER_ENABLED=true) so it is never exposed through the public proxy by accident.
	if c.SwaggerEnabled {
		app.Get("/swagger/*", swaggo.HandlerDefault)
	}

	// Register HTTP routes per module
	system.RegisterHTTPRoutes(app, sysService, authMw)
	authMod.RegisterHTTPRoutes(app, authService, limiter, log, authMw)
	logs.RegisterHTTPRoutes(app, logsService, log, authMw)
	analytics.RegisterHTTPRoutes(app, analyticsService, log, authMw)
	guides.RegisterHTTPRoutes(app, guidesService, log, authMw)
	achievements.RegisterHTTPRoutes(app, achievementsService, log, authMw)
	photos.RegisterHTTPRoutes(app, photosService, limiter, log, authMw)
	aiMod.RegisterHTTPRoutes(app, aiService, limiter, log, authMw)

	s := &Server{
		App:     app,
		DB:      db,
		Config:  c,
		Signer:  signer,
		AI:      aiClient,
		Limiter: limiter,
		Now:     time.Now,
		Log:     log,
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	s.Limiter.SetNowFunc(s.Now)
	return adaptor.FiberApp(s.App)
}

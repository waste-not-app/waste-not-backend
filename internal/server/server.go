package server

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	swaggo "github.com/gofiber/contrib/v3/swaggo"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	_ "github.com/waste-not-app/waste-not-backend/docs"
	"github.com/waste-not-app/waste-not-backend/internal/aiclient"
	"github.com/waste-not-app/waste-not-backend/internal/config"
	"github.com/waste-not-app/waste-not-backend/internal/cursor"
	"github.com/waste-not-app/waste-not-backend/internal/middleware"
	"github.com/waste-not-app/waste-not-backend/internal/modules/achievements"
	"github.com/waste-not-app/waste-not-backend/internal/modules/ai"
	"github.com/waste-not-app/waste-not-backend/internal/modules/analytics"
	"github.com/waste-not-app/waste-not-backend/internal/modules/auth"
	"github.com/waste-not-app/waste-not-backend/internal/modules/guides"
	"github.com/waste-not-app/waste-not-backend/internal/modules/logs"
	"github.com/waste-not-app/waste-not-backend/internal/modules/photos"
	"github.com/waste-not-app/waste-not-backend/internal/modules/system"
	"github.com/waste-not-app/waste-not-backend/internal/ratelimit"
	"github.com/waste-not-app/waste-not-backend/internal/response"
	"github.com/waste-not-app/waste-not-backend/internal/security"
)

type Server struct {
	App     *fiber.App
	DB      *pgxpool.Pool
	Config  config.Config
	Signer  security.Signer
	AI      *aiclient.Client
	Limiter *ratelimit.Limiter
	Log     *slog.Logger
	cancel  context.CancelFunc
	workers sync.WaitGroup
}

func New(db *pgxpool.Pool, c config.Config, log *slog.Logger) (*Server, error) {
	mailer, err := auth.NewSMTPMailer(c)
	if err != nil {
		return nil, err
	}
	return newServer(db, c, log, mailer)
}

func newServer(db *pgxpool.Pool, c config.Config, log *slog.Logger, mailer auth.ResetMailer) (*Server, error) {
	aiClient, err := aiclient.New(c)
	if err != nil {
		return nil, err
	}
	dummyHash, err := security.Hash(uuid.NewString())
	if err != nil {
		return nil, err
	}

	signer := security.Signer{
		Secret:   c.Secret,
		Issuer:   c.Issuer,
		Audience: c.Audience,
	}
	limiter := ratelimit.NewLimiter(db, time.Now)
	cursorCodec := cursor.NewCodec(c.Secret)

	app := fiber.New(fiber.Config{
		AppName:      "WasteNot API v1",
		BodyLimit:    6 * 1024 * 1024,
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

	sysRepo := system.NewRepository(db)
	sysService := system.NewService(sysRepo, c, aiClient)

	authRepo := auth.NewRepository(db, photoStorage)
	authService := auth.NewService(authRepo, signer, time.Now, dummyHash, mailer)

	logsRepo := logs.NewRepository(db, photoStorage)
	logsService := logs.NewService(logsRepo, c, time.Now, cursorCodec)

	analyticsRepo := analytics.NewRepository(db)
	analyticsService := analytics.NewService(analyticsRepo, time.Now)

	guidesRepo := guides.NewRepository(db)
	guidesService := guides.NewService(guidesRepo, cursorCodec)

	achievementsRepo := achievements.NewRepository(db)
	achievementsService := achievements.NewService(achievementsRepo, time.Now)

	photosRepo := photos.NewRepository(db)
	photosService := photos.NewService(photosRepo, photoStorage)

	aiRepo := ai.NewRepository(db)
	aiService := ai.NewService(aiRepo, c, aiClient, limiter, time.Now)

	if c.SwaggerEnabled {
		app.Get("/swagger/*", swaggo.HandlerDefault)
	}

	system.RegisterHTTPRoutes(app, sysService, authMw)
	auth.RegisterHTTPRoutes(app, authService, limiter, log, authMw)
	logs.RegisterHTTPRoutes(app, logsService, log, authMw)
	analytics.RegisterHTTPRoutes(app, analyticsService, log, authMw)
	guides.RegisterHTTPRoutes(app, guidesService, log, authMw)
	achievements.RegisterHTTPRoutes(app, achievementsService, log, authMw)
	photos.RegisterHTTPRoutes(app, photosService, limiter, log, authMw)
	ai.RegisterHTTPRoutes(app, aiService, limiter, log, authMw)

	s := &Server{
		App:     app,
		DB:      db,
		Config:  c,
		Signer:  signer,
		AI:      aiClient,
		Limiter: limiter,
		Log:     log,
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.workers.Add(2)
	go func() {
		defer s.workers.Done()
		authService.RunResetMail(ctx, log)
	}()
	go func() {
		defer s.workers.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleanup, stop := context.WithTimeout(ctx, 20*time.Second)
				err := photos.DrainCleanup(cleanup, db, photoStorage, "", 100)
				stop()
				if err != nil && ctx.Err() == nil && log != nil {
					log.Warn("private photo cleanup pending; storage or queue unavailable")
				}
			}
		}
	}()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return adaptor.FiberApp(s.App)
}

func (s *Server) Close() {
	s.cancel()
	s.workers.Wait()
}

package builderendpoint

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint/config"
	"github.com/ssvlabs/ssv/mev/builderendpoint/domain"
	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi"
)

// Server is the application-layer entrypoint for the SSV-hosted Builder API endpoint.
// Transport details live in `httpapi`, and config lives in `config`.
type Server struct {
	logger     *zap.Logger
	httpServer *http.Server
}

func New(logger *zap.Logger, cfg config.Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	handler := httpapi.NewRouter(httpapi.Dependencies{
		Logger:      logger,
		BidProvider: domain.NoopBidProvider{},
	})

	return &Server{
		logger: logger,
		httpServer: &http.Server{
			Addr:              cfg.ListenAddress,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
		},
	}, nil
}

// Run serves until ctx is canceled or the underlying server returns an error.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		if s.logger != nil {
			s.logger.Info("serving builder endpoint", zap.String("addr", s.httpServer.Addr))
		}
		errCh <- s.httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

package middleware

import (
	"log/slog"
	"time"

	"tygor.dev/tygor"
)

// LoggingInterceptor creates an interceptor that logs service calls using slog.
// It logs the start and end of each call, including duration and error status.
func LoggingInterceptor(logger *slog.Logger) tygor.UnaryInterceptor {
	if logger == nil {
		logger = slog.Default()
	}

	return func(ctx tygor.Context, req any, handler tygor.HandlerFunc) (any, error) {
		start := time.Now()

		logger.InfoContext(ctx, "request started",
			slog.String("endpoint", ctx.EndpointID()),
		)

		res, err := handler(ctx, req)
		duration := time.Since(start)

		if err != nil {
			logger.ErrorContext(ctx, "request failed",
				slog.String("endpoint", ctx.EndpointID()),
				slog.Duration("duration", duration),
				slog.Any("error", err),
			)
		} else {
			logger.InfoContext(ctx, "request completed",
				slog.String("endpoint", ctx.EndpointID()),
				slog.Duration("duration", duration),
			)
		}

		return res, err
	}
}

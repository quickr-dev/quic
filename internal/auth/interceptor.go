package auth

import (
	"context"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type contextKey string

const UserContextKey contextKey = "user"

func UnaryAuthInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		authHeaders := md.Get("authorization")
		if len(authHeaders) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header")
		}

		token := ExtractTokenFromMetadata(authHeaders[0])
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization header format")
		}

		userName, err := ValidateToken(token)
		if err != nil {
			log.Printf("Authentication failed for token %s...: %v", token[:min(8, len(token))], err)
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		log.Printf("Authenticated user: %s", userName)

		newCtx := context.WithValue(ctx, UserContextKey, userName)

		return handler(newCtx, req)
	}
}

func GetUserFromContext(ctx context.Context) (string, bool) {
	user, ok := ctx.Value(UserContextKey).(string)
	return user, ok
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

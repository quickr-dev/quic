package auth

import (
	"fmt"
	"strings"

	"github.com/quickr-dev/quic/internal/db"
)

func ValidateToken(token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("token is required")
	}

	database, err := db.InitDB()
	if err != nil {
		return "", fmt.Errorf("initializing database: %w", err)
	}
	defer database.Close()

	user, err := database.GetUserByToken(token)
	if err != nil {
		return "", err
	}

	return user.Name, nil
}

func ExtractTokenFromHeader(authHeader string) string {
	if authHeader == "" {
		return ""
	}

	const bearerPrefix = "Bearer "
	if strings.HasPrefix(authHeader, bearerPrefix) {
		return strings.TrimPrefix(authHeader, bearerPrefix)
	}

	return ""
}

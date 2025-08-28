package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

type AuthConfig struct {
	Users []User `json:"users"`
}

const DefaultAuthConfigPath = "/etc/quic/auth.json"

func ValidateToken(token string) (string, error) {
	return ValidateTokenFromFile(token, DefaultAuthConfigPath)
}

func ValidateTokenFromFile(token string, configPath string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("token is required")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("reading auth config: %w", err)
	}

	var config AuthConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("parsing auth config: %w", err)
	}

	for _, user := range config.Users {
		if bcrypt.CompareHashAndPassword([]byte(user.Token), []byte(token)) == nil {
			return user.Name, nil
		}
	}

	return "", fmt.Errorf("invalid token")
}

func ExtractTokenFromMetadata(authHeader string) string {
	if authHeader == "" {
		return ""
	}

	const bearerPrefix = "Bearer "
	if strings.HasPrefix(authHeader, bearerPrefix) {
		return strings.TrimPrefix(authHeader, bearerPrefix)
	}

	return ""
}

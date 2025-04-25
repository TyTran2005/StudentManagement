package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"student-management-api/internal/config"

	"github.com/golang-jwt/jwt/v4"
	"gorm.io/gorm"
)

type HasuraClaims struct {
	HasuraNamespace map[string]interface{} `json:"https://hasura.io/jwt/claims"`
	jwt.RegisteredClaims
}

type AuthWebhookResponse struct {
	UserID string `json:"X-Hasura-User-Id"`
	Role   string `json:"X-Hasura-Role"`
	Status string `json:"status"`
}

func AuthWebhookHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Println("INFO: AuthWebhookHandler: Received request")

		authHeader := r.Header.Get("Authorization")
		tokenString := ""
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
		}

		if tokenString == "" {
			log.Println("WARN: AuthWebhookHandler: No Authorization token found.")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"message": "Unauthorized: Missing token"})
			return
		}

		jwtType := config.AppConfig.HasuraJWTType
		jwtKey := config.AppConfig.HasuraJWTKey
		jwkURL := config.AppConfig.HasuraJWKURL

		claims := &HasuraClaims{}
		keyFunc := func(token *jwt.Token) (interface{}, error) {
			switch jwtType {
			case "HS256":
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}
				if jwtKey == "" {
					return nil, errors.New("HS256 key is missing in config")
				}
				return []byte(jwtKey), nil
			case "RS256":
				log.Println("WARN: AuthWebhookHandler: RS256 validation needs implementation using JWK URL:", jwkURL)
				return nil, errors.New("RS256 validation not implemented")
			default:
				return nil, fmt.Errorf("unsupported jwt type: %s", jwtType)
			}
		}

		token, err := jwt.ParseWithClaims(tokenString, claims, keyFunc)
		if err != nil {
			log.Printf("WARN: AuthWebhookHandler: Invalid Token - %v", err)
			errMsg := "Invalid Token"
			if errors.Is(err, jwt.ErrTokenExpired) {
				errMsg = "Token has expired"
			} else if validationErr, ok := err.(*jwt.ValidationError); ok {
				if validationErr.Errors&jwt.ValidationErrorSignatureInvalid != 0 {
					errMsg = "Invalid token signature"
				} else if validationErr.Errors&jwt.ValidationErrorMalformed != 0 {
					errMsg = "Malformed token"
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"message": errMsg})
			return
		}

		if !token.Valid {
			log.Println("WARN: AuthWebhookHandler: Token parsed but invalid.")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"message": "Invalid Token"})
			return
		}

		hasuraClaims := claims.HasuraNamespace
		if hasuraClaims == nil {
			log.Println("ERROR: AuthWebhookHandler: Missing 'https://hasura.io/jwt/claims' namespace.")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"message": "Forbidden: Missing required claims namespace"})
			return
		}

		userIDStr, okUserID := hasuraClaims["x-hasura-user-id"].(string)
		defaultRole, okRole := hasuraClaims["x-hasura-default-role"].(string)

		if !okUserID || userIDStr == "" || !okRole || defaultRole == "" {
			log.Println("ERROR: AuthWebhookHandler: Missing or invalid user-id or default-role in claims.")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"message": "Forbidden: Invalid claims data"})
			return
		}
		response := AuthWebhookResponse{
			UserID: userIDStr,
			Role:   defaultRole,
			Status: "success",
		}

		log.Printf("INFO: AuthWebhookHandler: Responding to Hasura with UserID: %s, Role: %s, Status: %s", response.UserID, response.Role, response.Status)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("ERROR: AuthWebhookHandler: Failed to encode response: %v", err)
		}
	}
}

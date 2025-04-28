package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"student-management-api/internal/config"

	"github.com/MicahParks/keyfunc"
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
	var jwks *keyfunc.JWKS
	if config.AppConfig.HasuraJWTType == "RS256" {
		var err error
		jwks, err = keyfunc.Get(config.AppConfig.HasuraJWKURL, keyfunc.Options{})
		if err != nil {
			log.Fatalf("FATAL: cannot load JWKs from %s: %v", config.AppConfig.HasuraJWKURL, err)
		}
		log.Println("INFO: JWKs loaded for RS256 validation.")
	}

	return func(w http.ResponseWriter, r *http.Request) {
		log.Println("INFO: AuthWebhookHandler: Received request")

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Unauthorized: Missing token", http.StatusUnauthorized)
			return
		}
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		claims := &HasuraClaims{}
		keyFunc := func(token *jwt.Token) (interface{}, error) {
			switch config.AppConfig.HasuraJWTType {
			case "HS256":
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}
				if config.AppConfig.HasuraJWTKey == "" {
					return nil, errors.New("HS256 key missing in config")
				}
				return []byte(config.AppConfig.HasuraJWTKey), nil

			case "RS256":
				if jwks == nil {
					return nil, errors.New("RS256 JWKs not initialized")
				}
				return jwks.Keyfunc(token)
			default:
				return nil, fmt.Errorf("unsupported jwt type: %s", config.AppConfig.HasuraJWTType)
			}
		}

		token, err := jwt.ParseWithClaims(tokenString, claims, keyFunc)
		if err != nil {
			log.Printf("WARN: Invalid token: %v", err)
			msg := "Invalid Token"
			if errors.Is(err, jwt.ErrTokenExpired) {
				msg = "Token has expired"
			}
			http.Error(w, msg, http.StatusUnauthorized)
			return
		}

		if !token.Valid {
			http.Error(w, "Invalid Token", http.StatusUnauthorized)
			return
		}

		ns := claims.HasuraNamespace
		if ns == nil {
			http.Error(w, "Forbidden: Missing required claims namespace", http.StatusForbidden)
			return
		}
		userID, ok1 := ns["x-hasura-user-id"].(string)
		role, ok2 := ns["x-hasura-default-role"].(string)
		if !ok1 || !ok2 || userID == "" || role == "" {
			http.Error(w, "Forbidden: Invalid claims data", http.StatusForbidden)
			return
		}
		resp := AuthWebhookResponse{UserID: userID, Role: role, Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		log.Printf("INFO: AuthWebhookHandler: ok user=%s role=%s", userID, role)
	}
}

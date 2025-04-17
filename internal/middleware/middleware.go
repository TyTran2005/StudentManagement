package middleware

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"student-management-api/internal/config"

	"github.com/golang-jwt/jwt/v4"
)

type contextKey string

const (
	UserIDKey contextKey = "userID"
	RoleKey   contextKey = "role"
)

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		tokenString := ""
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
		}

		if tokenString == "" {
			log.Println("DEBUG: No valid Authorization header found, proceeding without authentication context.")
			next.ServeHTTP(w, r)
			return
		}

		if config.AppConfig == nil || config.AppConfig.JWTSecretKey == "" {
			log.Println("ERROR: JWT Secret Key is not configured in AuthMiddleware.")
			next.ServeHTTP(w, r)
			return
		}
		jwtSecret := []byte(config.AppConfig.JWTSecretKey)

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			log.Printf("DEBUG: Invalid Token: %v. Proceeding without authentication context.", err)

			next.ServeHTTP(w, r)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			log.Println("ERROR: Invalid token claims type. Proceeding without authentication context.")
			next.ServeHTTP(w, r)
			return
		}

		userIDFloat, okUserID := claims["userID"].(float64)

		role, okRole := claims["role"].(bool)

		if !okUserID || !okRole {
			log.Println("ERROR: Invalid userID or role in token claims. Proceeding without authentication context.")
			next.ServeHTTP(w, r)
			return
		}

		userID := uint(userIDFloat)

		log.Printf("DEBUG: Authenticated userID: %d, role: %v", userID, role)

		ctx := context.WithValue(r.Context(), UserIDKey, userID)
		ctx = context.WithValue(ctx, RoleKey, role)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetUserIDFromContext(ctx context.Context) (uint, bool) {
	userID, ok := ctx.Value(UserIDKey).(uint)
	return userID, ok && userID > 0
}

func GetRoleFromContext(ctx context.Context) (bool, bool) {
	role, ok := ctx.Value(RoleKey).(bool)
	return role, ok
}

package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"student-management-api/internal/config"

	"github.com/golang-jwt/jwt/v4"
)

type contextKey string

const (
	UserIDKey       contextKey = "userID"
	RoleKey         contextKey = "role"
	AllowedRolesKey contextKey = "allowedRoles"
)

type HasuraClaims struct {
	HasuraNamespace map[string]interface{} `json:"https://hasura.io/jwt/claims"`
	jwt.RegisteredClaims
}

type graphQLRequestBody struct {
	Query         string                 `json:"query"`
	OperationName string                 `json:"operationName"`
	Variables     map[string]interface{} `json:"variables"`
}

func sendGraphQLError(w http.ResponseWriter, message string, statusCode int, errorCode string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	jsonError := map[string]interface{}{
		"errors": []map[string]interface{}{
			{
				"message": message,
				"extensions": map[string]interface{}{
					"code": errorCode,
				},
			},
		},
	}
	if err := json.NewEncoder(w).Encode(jsonError); err != nil {
		log.Printf("ERROR: sendGraphQLError: Failed to encode JSON error response: %v", err)
	}
}

var publicOperations = map[string]bool{
	"LoginUser":          true,
	"RegisterUser":       true,
	"IntrospectionQuery": true,
}

func isPublicOperation(r *http.Request) (bool, error) {
	if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
		return false, nil
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("ERROR: [isPublicOperation] Failed to read request body: %v", err)
		return false, fmt.Errorf("failed to read request body for public check: %w", err)
	}

	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	log.Printf("DEBUG: [isPublicOperation] Received Request Body: %s", string(bodyBytes))

	var reqBody graphQLRequestBody
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
		log.Printf("DEBUG: [isPublicOperation] Failed to parse GraphQL JSON body: %v. Assuming not public.", err)
		return false, nil
	}

	log.Printf("DEBUG: [isPublicOperation] Parsed OperationName: '%s'", reqBody.OperationName)
	trimmedQuery := strings.TrimSpace(reqBody.Query)
	log.Printf("DEBUG: [isPublicOperation] Parsed Query (trimmed): '%s'", trimmedQuery)

	if reqBody.OperationName != "" {
		isPublic := publicOperations[reqBody.OperationName]
		log.Printf("DEBUG: [isPublicOperation] Checking OperationName '%s'. Is public? %t", reqBody.OperationName, isPublic)
		if isPublic {
			return true, nil
		}
		log.Println("DEBUG: [isPublicOperation] OperationName provided but not public.")
		return false, nil
	} else {
		log.Println("DEBUG: [isPublicOperation] No OperationName provided, checking query string.")

		if strings.Contains(trimmedQuery, "IntrospectionQuery") {
			log.Println("DEBUG: [isPublicOperation] Query contains IntrospectionQuery. Allowing.")
			return true, nil
		}
	}

	log.Println("DEBUG: [isPublicOperation] Operation determined to be NOT public.")
	return false, nil
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		bodyBytes, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			log.Printf("CRITICAL: AuthMiddleware: Failed to read request body initially: %v", readErr)
			sendGraphQLError(w, "Internal Server Error: Cannot process request body", http.StatusInternalServerError, "INTERNAL_SERVER_ERROR")
			return
		}
		r.Body.Close()

		rForCheck := r.Clone(context.Background())
		rForCheck.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		isPublic, checkErr := isPublicOperation(rForCheck)
		if checkErr != nil {
			log.Printf("ERROR: AuthMiddleware: Error during isPublicOperation check: %v", checkErr)
			sendGraphQLError(w, "Internal Server Error: Cannot determine operation public status", http.StatusInternalServerError, "INTERNAL_SERVER_ERROR")
			return
		}
		if isPublic {
			log.Println("DEBUG: AuthMiddleware: Skipping authentication for public GraphQL operation.")
			next.ServeHTTP(w, r)
			return
		}
		log.Println("DEBUG: AuthMiddleware: Starting authentication check for protected GraphQL operation.")

		if config.AppConfig == nil {
			log.Println("CRITICAL: AuthMiddleware: Config not loaded.")
			http.Error(w, "Internal Server Error: Auth config missing", http.StatusInternalServerError)
			return
		}
		jwtType := config.AppConfig.HasuraJWTType
		jwtKey := config.AppConfig.HasuraJWTKey
		jwkURL := config.AppConfig.HasuraJWKURL
		if jwtType == "HS256" && jwtKey == "" {
			log.Println("CRITICAL: AuthMiddleware: JWT type HS256 but key is missing.")
			http.Error(w, "Internal Server Error: Auth config error", http.StatusInternalServerError)
			return
		}

		authHeader := r.Header.Get("Authorization")
		tokenString := ""
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
		}

		if tokenString == "" {
			log.Println("DEBUG: AuthMiddleware: Authorization token not found for protected operation.")
			sendGraphQLError(w, "Authorization Required: Token missing", http.StatusUnauthorized, "UNAUTHENTICATED")
			return
		}

		claims := &HasuraClaims{}
		keyFunc := func(token *jwt.Token) (interface{}, error) {

			switch jwtType {
			case "HS256":
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v, expected %s", token.Header["alg"], jwt.SigningMethodHS256.Alg())
				}
				return []byte(jwtKey), nil
			case "RS256":
				if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v, expected %s", token.Header["alg"], jwt.SigningMethodRS256.Alg())
				}
				log.Println("WARN: AuthMiddleware: RS256 validation logic using JWK URL is required:", jwkURL)
				return nil, errors.New("RS256 validation not implemented")
			default:
				return nil, fmt.Errorf("unsupported jwt type configured: %s", jwtType)
			}
		}

		token, err := jwt.ParseWithClaims(tokenString, claims, keyFunc)

		if err != nil {

			log.Printf("WARN: AuthMiddleware: Invalid Token - %v", err)
			errMsg := "Invalid Token"
			errCode := "INVALID_TOKEN"
			if errors.Is(err, jwt.ErrTokenExpired) {
				errMsg = "Token has expired"
				errCode = "TOKEN_EXPIRED"
			} else if errors.Is(err, jwt.ErrTokenSignatureInvalid) {
				errMsg = "Invalid token signature"
			} else if strings.Contains(err.Error(), "unexpected signing method") {
				errMsg = "Invalid token signing method"
			}
			sendGraphQLError(w, errMsg, http.StatusUnauthorized, errCode)
			return
		}
		if !token.Valid {
			log.Println("WARN: AuthMiddleware: Token parsed but marked invalid.")
			sendGraphQLError(w, "Invalid Token", http.StatusUnauthorized, "INVALID_TOKEN")
			return
		}

		hasuraClaims := claims.HasuraNamespace
		if hasuraClaims == nil {
			log.Println("ERROR: AuthMiddleware: Missing 'https://hasura.io/jwt/claims' namespace.")
			sendGraphQLError(w, "Forbidden: Missing required claims namespace", http.StatusForbidden, "FORBIDDEN_MISSING_CLAIMS")
			return
		}
		userIDStr, okUserID := hasuraClaims["x-hasura-user-id"].(string)
		if !okUserID || userIDStr == "" {
			log.Println("ERROR: AuthMiddleware: Missing or invalid 'x-hasura-user-id' (string).")
			sendGraphQLError(w, "Forbidden: Invalid user claim", http.StatusForbidden, "FORBIDDEN_INVALID_USER_CLAIM")
			return
		}
		defaultRole, okRole := hasuraClaims["x-hasura-default-role"].(string)
		if !okRole || defaultRole == "" {
			log.Println("ERROR: AuthMiddleware: Missing or invalid 'x-hasura-default-role' (string).")
			sendGraphQLError(w, "Forbidden: Invalid role claim", http.StatusForbidden, "FORBIDDEN_INVALID_ROLE_CLAIM")
			return
		}
		allowedRoles := []string{}
		allowedRolesRaw, okAllowed := hasuraClaims["x-hasura-allowed-roles"].([]interface{})
		if !okAllowed {
			log.Println("ERROR: AuthMiddleware: Missing 'x-hasura-allowed-roles' claim.")
			sendGraphQLError(w, "Forbidden: Missing allowed roles claim", http.StatusForbidden, "FORBIDDEN_MISSING_ALLOWED_ROLES")
			return
		}
		validRoleFound := false
		for _, r := range allowedRolesRaw {
			if roleStr, ok := r.(string); ok {
				allowedRoles = append(allowedRoles, roleStr)
				if roleStr == defaultRole {
					validRoleFound = true
				}
			} else {
				log.Printf("WARN: AuthMiddleware: Non-string value found in 'x-hasura-allowed-roles': %T %v", r, r)
			}
		}
		if len(allowedRoles) == 0 {
			log.Println("ERROR: AuthMiddleware: 'x-hasura-allowed-roles' is empty after processing.")
			sendGraphQLError(w, "Forbidden: No valid allowed roles specified", http.StatusForbidden, "FORBIDDEN_NO_VALID_ROLES")
			return
		}
		if !validRoleFound {
			log.Printf("ERROR: AuthMiddleware: Default role '%s' not found in allowed roles: %v", defaultRole, allowedRoles)
			sendGraphQLError(w, "Forbidden: Default role not allowed", http.StatusForbidden, "FORBIDDEN_DEFAULT_ROLE_NOT_ALLOWED")
			return
		}

		ctx := context.WithValue(r.Context(), UserIDKey, userIDStr)
		ctx = context.WithValue(ctx, RoleKey, defaultRole)
		ctx = context.WithValue(ctx, AllowedRolesKey, allowedRoles)
		log.Printf("DEBUG: AuthMiddleware: Authenticated UserID: %s, Role: %s, AllowedRoles: %v", userIDStr, defaultRole, allowedRoles)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetUserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(UserIDKey).(string)
	return userID, ok && userID != ""
}

func GetRoleFromContext(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(RoleKey).(string)
	return role, ok && role != ""
}

func GetAllowedRolesFromContext(ctx context.Context) ([]string, bool) {
	roles, ok := ctx.Value(AllowedRolesKey).([]string)
	return roles, ok && len(roles) > 0
}

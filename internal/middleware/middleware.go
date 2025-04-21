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
		log.Printf("ERROR: [isPublicOperation] Không thể đọc request body: %v", err)

		return false, fmt.Errorf("failed to read request body")
	}
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var reqBody graphQLRequestBody
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {

		log.Printf("DEBUG: [isPublicOperation] Không parse được GraphQL JSON body: %v. Coi như không phải public.", err)
		return false, nil
	}

	log.Printf("DEBUG: [isPublicOperation] Parsed OperationName: '%s'", reqBody.OperationName)

	if reqBody.OperationName != "" {
		isPublic := publicOperations[reqBody.OperationName]
		log.Printf("DEBUG: [isPublicOperation] Kiểm tra OperationName '%s'. Là public? %t", reqBody.OperationName, isPublic)
		if isPublic {
			return true, nil
		}
	} else {

		log.Println("DEBUG: [isPublicOperation] Không có OperationName, kiểm tra query string.")
		queryTrimmed := strings.TrimSpace(reqBody.Query)

		if strings.HasPrefix(queryTrimmed, "mutation LoginUser") || strings.HasPrefix(queryTrimmed, "mutation loginUser") {
			log.Println("DEBUG: [isPublicOperation] Query prefix khớp LoginUser. Cho phép.")
			return true, nil
		}
		if strings.HasPrefix(queryTrimmed, "mutation RegisterUser") || strings.HasPrefix(queryTrimmed, "mutation registerUser") {
			log.Println("DEBUG: [isPublicOperation] Query prefix khớp RegisterUser. Cho phép.")
			return true, nil
		}

		if strings.Contains(queryTrimmed, "IntrospectionQuery") {
			log.Println("DEBUG: [isPublicOperation] Query chứa IntrospectionQuery. Cho phép.")
			return true, nil
		}
	}

	log.Println("DEBUG: [isPublicOperation] Operation được xác định là KHÔNG public.")
	return false, nil
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		isPublic, readErr := isPublicOperation(r)
		if readErr != nil {
			log.Printf("CRITICAL: AuthMiddleware: Lỗi đọc body request: %v", readErr)
			http.Error(w, "Internal Server Error: Cannot process request", http.StatusInternalServerError)
			return
		}

		if isPublic {
			log.Println("DEBUG: AuthMiddleware: Bỏ qua xác thực cho public GraphQL operation.")
			next.ServeHTTP(w, r)
			return
		}

		log.Println("DEBUG: AuthMiddleware: Bắt đầu kiểm tra xác thực cho protected GraphQL operation.")

		if config.AppConfig == nil {
			log.Println("CRITICAL: AuthMiddleware: Config chưa được load.")
			http.Error(w, "Internal Server Error: Auth config missing", http.StatusInternalServerError)
			return
		}
		jwtType := config.AppConfig.HasuraJWTType
		jwtKey := config.AppConfig.HasuraJWTKey
		jwkURL := config.AppConfig.HasuraJWKURL
		if jwtType == "HS256" && jwtKey == "" {
			log.Println("CRITICAL: AuthMiddleware: JWT type HS256 nhưng key bị thiếu.")
			http.Error(w, "Internal Server Error: Auth config error", http.StatusInternalServerError)
			return
		}

		authHeader := r.Header.Get("Authorization")
		tokenString := ""
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
		}

		if tokenString == "" {
			log.Println("DEBUG: AuthMiddleware: Không tìm thấy Authorization token cho protected operation.")
			w.Header().Set("WWW-Authenticate", `Bearer realm="protected api"`)
			http.Error(w, "Authorization Required", http.StatusUnauthorized)
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
				log.Println("WARN: AuthMiddleware: Cần implement logic lấy public key từ JWK URL cho RS256:", jwkURL)
				return nil, errors.New("RS256 validation not implemented")
			default:
				return nil, fmt.Errorf("unsupported jwt type configured: %s", jwtType)
			}
		}
		token, err := jwt.ParseWithClaims(tokenString, claims, keyFunc)
		if err != nil {

			log.Printf("WARN: AuthMiddleware: Invalid Token - %v", err)
			errMsg := "Invalid Token"
			if errors.Is(err, jwt.ErrTokenExpired) {
				errMsg = "Token has expired"
			} else if errors.Is(err, jwt.ErrTokenSignatureInvalid) {
				errMsg = "Invalid token signature"
			} else if strings.Contains(err.Error(), "unexpected signing method") {
				errMsg = "Invalid token signing method"
			}
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="`+errMsg+`"`)
			http.Error(w, errMsg, http.StatusUnauthorized)
			return
		}
		if !token.Valid {

			log.Println("WARN: AuthMiddleware: Token parsed nhưng không hợp lệ.")
			http.Error(w, "Invalid Token", http.StatusUnauthorized)
			return
		}

		hasuraClaims := claims.HasuraNamespace
		if hasuraClaims == nil {
			log.Println("ERROR: AuthMiddleware: Thiếu namespace 'https://hasura.io/jwt/claims'.")
			http.Error(w, "Forbidden: Missing required claims namespace", http.StatusForbidden)
			return
		}
		userIDStr, okUserID := hasuraClaims["x-hasura-user-id"].(string)
		if !okUserID || userIDStr == "" {
			log.Println("ERROR: AuthMiddleware: Thiếu hoặc sai định dạng 'x-hasura-user-id' (string).")
			http.Error(w, "Forbidden: Invalid user claim", http.StatusForbidden)
			return
		}
		defaultRole, okRole := hasuraClaims["x-hasura-default-role"].(string)
		if !okRole || defaultRole == "" {
			log.Println("ERROR: AuthMiddleware: Thiếu hoặc sai định dạng 'x-hasura-default-role' (string).")
			http.Error(w, "Forbidden: Invalid role claim", http.StatusForbidden)
			return
		}
		allowedRoles := []string{}
		allowedRolesRaw, okAllowed := hasuraClaims["x-hasura-allowed-roles"].([]interface{})
		if !okAllowed {
			log.Println("ERROR: AuthMiddleware: Thiếu claim 'x-hasura-allowed-roles'.")
			http.Error(w, "Forbidden: Missing allowed roles claim", http.StatusForbidden)
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
				log.Printf("WARN: AuthMiddleware: Giá trị không phải string trong 'x-hasura-allowed-roles': %T %v", r, r)
			}
		}
		if len(allowedRoles) == 0 {
			log.Println("ERROR: AuthMiddleware: 'x-hasura-allowed-roles' rỗng sau khi xử lý.")
			http.Error(w, "Forbidden: No valid allowed roles specified", http.StatusForbidden)
			return
		}
		if !validRoleFound {
			log.Printf("ERROR: AuthMiddleware: Default role '%s' không có trong allowed roles: %v", defaultRole, allowedRoles)
			http.Error(w, "Forbidden: Default role not allowed", http.StatusForbidden)
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
	return roles, ok
}

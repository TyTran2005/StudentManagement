package resolvers

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"student-management-api/internal/config"
	"student-management-api/internal/middleware"
	"student-management-api/internal/models"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/graphql-go/graphql"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var DB *gorm.DB

func requireRole(params graphql.ResolveParams, requiredRole string) (string, error) {
	userIDStr, ok := middleware.GetUserIDFromContext(params.Context)
	if !ok {
		return "", errors.New("authorization required: user ID not found in context")
	}
	actualRole, ok := middleware.GetRoleFromContext(params.Context)
	if !ok {
		log.Printf("ERROR: requireRole: Role not found in context for userID %s", userIDStr)
		return "", errors.New("internal authorization error: role missing from context")
	}
	if actualRole != requiredRole {
		return "", fmt.Errorf("authorization error: %s access required (current role: %s)", requiredRole, actualRole)
	}
	return userIDStr, nil
}

func requireTeacher(params graphql.ResolveParams) (string, error) {
	return requireRole(params, "teacher")
}

func requireStudent(params graphql.ResolveParams) (string, error) {
	return requireRole(params, "student")
}

func requireAuth(params graphql.ResolveParams) (string, string, error) {
	userIDStr, ok := middleware.GetUserIDFromContext(params.Context)
	if !ok {
		return "", "", errors.New("authorization required: user ID not found in context")
	}
	role, ok := middleware.GetRoleFromContext(params.Context)
	if !ok {
		log.Printf("ERROR: requireAuth: Role not found in context for userID %s", userIDStr)
		return "", "", errors.New("internal authorization error: role missing from context")
	}
	return userIDStr, role, nil
}

func getUserIDUint(userIDStr string) (uint, error) {
	userIDUint64, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		log.Printf("ERROR: getUserIDUint: Could not parse user ID string '%s' to uint: %v", userIDStr, err)
		return 0, errors.New("invalid user ID format encountered")
	}
	if userIDUint64 == 0 {
		log.Printf("ERROR: getUserIDUint: Parsed user ID is 0 from string '%s'", userIDStr)
		return 0, errors.New("invalid user ID (0) encountered")
	}
	return uint(userIDUint64), nil
}

func GetCurrentUser(params graphql.ResolveParams) (interface{}, error) {
	userIDStr, _, err := requireAuth(params)
	if err != nil {
		return nil, err
	}
	userID, err := getUserIDUint(userIDStr)
	if err != nil {
		return nil, err
	}

	var user models.User
	if err := DB.Select("id", "fullname", "email", "role", "created_at", "updated_at").First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("WARNING: GetCurrentUser: User %d (from token ID %s) not found in DB", userID, userIDStr)
			return nil, errors.New("user associated with token not found")
		}
		log.Printf("ERROR: GetCurrentUser: Failed fetch user %d: %v", userID, err)
		return nil, errors.New("failed to retrieve user data")
	}
	return user, nil
}

func GetUsers(params graphql.ResolveParams) (interface{}, error) {
	_, err := requireTeacher(params)
	if err != nil {
		return nil, err
	}

	var users []models.User
	if err := DB.Select("id", "fullname", "email", "role", "created_at", "updated_at").Find(&users).Error; err != nil {
		log.Printf("ERROR: GetUsers: Failed: %v", err)
		return nil, errors.New("failed to retrieve users")
	}
	return users, nil
}

func GetClasses(params graphql.ResolveParams) (interface{}, error) {
	var classes []models.Class
	name, hasName := params.Args["name"].(string)
	leaderName, hasLeaderName := params.Args["leaderName"].(string)
	status, hasStatus := params.Args["status"].(bool)

	query := DB.Preload("Teacher", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).Preload("Leader", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	})

	if hasName && name != "" {
		query = query.Where("LOWER(name) LIKE LOWER(?)", "%"+name+"%")
	}
	if hasStatus {
		query = query.Where("status = ?", status)
	}
	if hasLeaderName && leaderName != "" {
		query = query.Joins("LEFT JOIN users AS leaders ON classes.leader_id = leaders.id").
			Where("LOWER(leaders.fullname) LIKE LOWER(?)", "%"+leaderName+"%")
	}

	if err := query.Find(&classes).Error; err != nil {
		log.Printf("ERROR: Failed to get classes: %v", err)
		return nil, errors.New("failed to retrieve classes")
	}
	return classes, nil
}

func GetOpenClasses(params graphql.ResolveParams) (interface{}, error) {
	var classes []models.Class
	err := DB.Preload("Teacher", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).Preload("Leader", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).Where("status = ?", true).Find(&classes).Error

	if err != nil {
		log.Printf("ERROR: Failed to retrieve open classes: %v", err)
		return nil, errors.New("failed to retrieve open classes")
	}
	return classes, nil
}

func GetRegisteredClasses(params graphql.ResolveParams) (interface{}, error) {
	studentIDStr, err := requireStudent(params)
	if err != nil {
		return nil, err
	}
	studentID, err := getUserIDUint(studentIDStr)
	if err != nil {
		return nil, err
	}
	name, hasName := params.Args["name"].(string)
	teacherName, hasTeacherName := params.Args["teacherName"].(string)

	query := DB.Table("classes").
		Select("classes.*").
		Joins("JOIN student_classes ON classes.id = student_classes.class_id AND student_classes.left_at IS NULL").
		Where("student_classes.student_id = ?", studentID).
		Preload("Teacher", func(db *gorm.DB) *gorm.DB {
			return db.Select("id", "fullname", "email", "role")
		}).
		Preload("Leader", func(db *gorm.DB) *gorm.DB {
			return db.Select("id", "fullname", "email", "role")
		})

	if hasName && name != "" {
		query = query.Where("LOWER(classes.name) LIKE LOWER(?)", "%"+name+"%")
	}
	if hasTeacherName && teacherName != "" {
		query = query.Joins("JOIN users AS teacher ON classes.teacher_id = teacher.id").
			Where("LOWER(teacher.fullname) LIKE LOWER(?)", "%"+teacherName+"%")
	}
	var classes []models.Class
	if err := query.Find(&classes).Error; err != nil {
		log.Printf("ERROR: GetRegisteredClasses: Failed for student %d: %v", studentID, err)
		return nil, errors.New("failed to retrieve registered classes")
	}
	return classes, nil
}

func GetStudentClassDetail(params graphql.ResolveParams) (interface{}, error) {
	studentIDStr, err := requireStudent(params)
	if err != nil {
		return nil, err
	}
	studentID, err := getUserIDUint(studentIDStr)
	if err != nil {
		return nil, err
	}

	classIDInput, ok := params.Args["classID"].(int)
	if !ok || classIDInput <= 0 {
		return nil, errors.New("invalid classID provided")
	}
	classID := uint(classIDInput)

	var studentClass models.StudentClass
	err = DB.Where("student_id = ? AND class_id = ? AND left_at IS NULL", studentID, classID).
		Preload("Class.Teacher", func(db *gorm.DB) *gorm.DB {
			return db.Select("id", "fullname", "email", "role")
		}).
		Preload("Class.Leader", func(db *gorm.DB) *gorm.DB {
			return db.Select("id", "fullname", "email", "role")
		}).
		Preload("Class").
		First(&studentClass).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("class not found or student not enrolled in this class")
		}
		log.Printf("ERROR: GetStudentClassDetail: Failed to fetch enrollment for student %d, class %d: %v", studentID, classID, err)
		return nil, errors.New("failed to retrieve class details")
	}

	if studentClass.Class.ID == 0 {
		log.Printf("ERROR: GetStudentClassDetail: Preloaded Class data has ID 0 for StudentClass (StudentID: %d, ClassID: %d)", studentID, classID)
		return nil, errors.New("failed to retrieve valid class details (internal data inconsistency)")
	}

	return studentClass.Class, nil
}

func GetClassDetail(params graphql.ResolveParams) (interface{}, error) {
	teacherIDStr, err := requireTeacher(params)
	if err != nil {
		return nil, err
	}
	teacherID, err := getUserIDUint(teacherIDStr)
	if err != nil {
		return nil, err
	}
	classIDInput, ok := params.Args["classID"].(int)
	if !ok || classIDInput <= 0 {
		return nil, errors.New("invalid classID provided")
	}
	classID := uint(classIDInput)

	var class models.Class
	err = DB.Preload("Teacher", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).Preload("Leader", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).Preload("StudentClasses", "left_at IS NULL").
		Preload("StudentClasses.Student", func(db *gorm.DB) *gorm.DB {
			return db.Select("id", "fullname", "email", "role")
		}).First(&class, classID).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("class not found")
		}
		log.Printf("ERROR: GetClassDetail: Failed for class %d by teacher %d: %v", classID, teacherID, err)
		return nil, errors.New("failed to retrieve class details")
	}
	if class.TeacherID == nil || *class.TeacherID != teacherID {
		log.Printf("WARN: GetClassDetail: Teacher %d accessed details for class %d owned by TeacherID %v", teacherID, classID, class.TeacherID)
		return nil, errors.New("authorization error: you are not the teacher of this class")
	}
	return class, nil
}

func RegisterUser(params graphql.ResolveParams) (interface{}, error) {
	fullname := params.Args["fullname"].(string)
	email := params.Args["email"].(string)
	password := params.Args["password"].(string)
	roleInput, roleProvided := params.Args["role"].(bool)

	if fullname == "" || email == "" || password == "" {
		return nil, errors.New("fullname, email, and password are required")
	}
	if !roleProvided {
		roleInput = false
	}

	var existingUser models.User
	err := DB.Where("email = ?", email).First(&existingUser).Error
	if err == nil {
		return nil, errors.New("email already exists")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("ERROR: RegisterUser Logic: DB error check email %s: %v", email, err)
		return nil, errors.New("database error checking email")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("ERROR: RegisterUser Logic: Failed hash password for %s: %v", email, err)
		return nil, errors.New("failed to process password")
	}

	user := models.User{
		Fullname: fullname,
		Email:    email,
		Password: string(hashedPassword),
		Role:     roleInput,
	}

	if err := DB.Create(&user).Error; err != nil {
		log.Printf("ERROR: RegisterUser Logic: Failed create user %s: %v", email, err)
		return nil, errors.New("failed to register user")
	}

	log.Printf("INFO: RegisterUser Logic: User registered: %s (ID: %d)", user.Email, user.ID)
	userResult := models.User{
		ID: user.ID, Fullname: user.Fullname, Email: user.Email,
		Role: user.Role, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
	return userResult, nil
}

func LoginUser(params graphql.ResolveParams) (interface{}, error) {
	email := params.Args["email"].(string)
	password := params.Args["password"].(string)

	if email == "" || password == "" {
		return nil, errors.New("email and password are required")
	}

	var user models.User
	if err := DB.Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("invalid email or password")
		}
		log.Printf("ERROR: LoginUser Logic: DB error find user %s: %v", email, err)
		return nil, errors.New("database error during login")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, errors.New("invalid email or password")
	}

	if config.AppConfig == nil || config.AppConfig.HasuraJWTKey == "" || config.AppConfig.HasuraJWTType != "HS256" {
		log.Println("ERROR: LoginUser Logic: Hasura JWT Key (HS256) not configured.")
		return nil, errors.New("internal server error: JWT configuration missing")
	}
	jwtSecret := []byte(config.AppConfig.HasuraJWTKey)
	expirationTime := time.Now().Add(24 * time.Hour)

	defaultRole := "student"
	allowedRoles := []string{"student"}
	if user.Role {
		defaultRole = "teacher"
		allowedRoles = []string{"teacher", "student"}
	}

	claims := jwt.MapClaims{
		"sub": user.Email,
		"iat": time.Now().Unix(),
		"exp": expirationTime.Unix(),
		"https://hasura.io/jwt/claims": map[string]interface{}{
			"x-hasura-allowed-roles": allowedRoles,
			"x-hasura-default-role":  defaultRole,
			"x-hasura-user-id":       strconv.FormatUint(uint64(user.ID), 10),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		log.Printf("ERROR: LoginUser Logic: Failed sign Hasura JWT for user %d: %v", user.ID, err)
		return nil, errors.New("failed to generate authentication token")
	}

	log.Printf("INFO: LoginUser Logic: User logged in: %s (ID: %d)", user.Email, user.ID)
	userResult := models.User{
		ID: user.ID, Fullname: user.Fullname, Email: user.Email,
		Role: user.Role, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
	return map[string]interface{}{
		"token": tokenString,
		"user":  userResult,
	}, nil
}

func UpdateUser(params graphql.ResolveParams) (interface{}, error) {
	userIDStr, _, err := requireAuth(params)
	if err != nil {
		return nil, err
	}
	userID, err := getUserIDUint(userIDStr)
	if err != nil {
		return nil, err
	}
	fullname, fullnameOk := params.Args["fullname"].(string)
	password, passwordOk := params.Args["password"].(string)

	if !fullnameOk && !passwordOk {
		return nil, errors.New("no fields provided for update (fullname or password required)")
	}

	var user models.User
	if err := DB.First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("WARNING: UpdateUser: User %d (from token ID %s) not found in DB", userID, userIDStr)
			return nil, errors.New("user not found")
		}
		log.Printf("ERROR: UpdateUser: Failed fetch user %d: %v", userID, err)
		return nil, errors.New("failed to retrieve user data")
	}

	updates := make(map[string]interface{})
	updated := false
	if fullnameOk && fullname != "" && user.Fullname != fullname {
		updates["Fullname"] = fullname
		updated = true
	}
	if passwordOk && password != "" {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("ERROR: UpdateUser: Failed hash new password for user %d: %v", userID, err)
			return nil, errors.New("failed to process new password")
		}
		updates["Password"] = string(hashedPassword)
		updated = true
	}

	if updated {
		updates["UpdatedAt"] = time.Now()
		if err := DB.Model(&user).Updates(updates).Error; err != nil {
			log.Printf("ERROR: UpdateUser: Failed update user %d: %v", userID, err)
			return nil, errors.New("failed to update user information")
		}
		log.Printf("INFO: UpdateUser: User %d updated.", userID)
	} else {
		log.Printf("INFO: UpdateUser: No changes for user %d.", userID)
	}

	userResult := models.User{
		ID: user.ID, Fullname: user.Fullname, Email: user.Email,
		Role: user.Role, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
	if updatedVal, ok := updates["UpdatedAt"]; ok {
		userResult.UpdatedAt = updatedVal.(time.Time)
	}
	return userResult, nil
}

func CreateClass(params graphql.ResolveParams) (interface{}, error) {
	teacherIDStr, err := requireTeacher(params)
	if err != nil {
		return nil, err
	}
	teacherID, err := getUserIDUint(teacherIDStr)
	if err != nil {
		return nil, err
	}
	name, _ := params.Args["name"].(string)
	subject, _ := params.Args["subject"].(string)
	statusInput, statusProvided := params.Args["status"].(bool)
	leaderIDInput, hasLeaderID := params.Args["leaderID"].(int)

	if name == "" || subject == "" {
		return nil, errors.New("class name and subject are required")
	}
	if !statusProvided {
		statusInput = true
	}

	class := models.Class{
		Name: name, Subject: subject, TeacherID: &teacherID, Status: statusInput,
	}

	if hasLeaderID && leaderIDInput > 0 {
		leaderID := uint(leaderIDInput)
		var potentialLeader models.User
		if err := DB.Select("id", "role").First(&potentialLeader, leaderID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("invalid leaderID: user %d not found", leaderID)
			}
			log.Printf("ERROR: CreateClass: DB error check leader %d by teacher %d: %v", leaderID, teacherID, err)
			return nil, errors.New("database error checking leader")
		}
		if potentialLeader.Role {
			return nil, fmt.Errorf("invalid leaderID: user %d is a teacher", leaderID)
		}
		class.LeaderID = &leaderID
	}

	if err := DB.Create(&class).Error; err != nil {
		log.Printf("ERROR: CreateClass: Failed for '%s' by teacher %d: %v", name, teacherID, err)
		return nil, errors.New("failed to create class")
	}
	log.Printf("INFO: CreateClass: Class '%s' (ID: %d) created by teacher %d", class.Name, class.ID, teacherID)

	var createdClass models.Class
	err = DB.Preload("Teacher", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).Preload("Leader", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).First(&createdClass, class.ID).Error
	if err != nil {
		log.Printf("WARNING: CreateClass: Failed preload associations class %d: %v", class.ID, err)
		return class, nil
	}
	return createdClass, nil
}

func UpdateClass(params graphql.ResolveParams) (interface{}, error) {
	teacherIDStr, err := requireTeacher(params)
	if err != nil {
		return nil, err
	}
	teacherID, err := getUserIDUint(teacherIDStr)
	if err != nil {
		return nil, err
	}
	classIDInput, ok := params.Args["classID"].(int)
	if !ok || classIDInput <= 0 {
		return nil, errors.New("invalid classID provided")
	}
	classID := uint(classIDInput)

	var class models.Class
	if err := DB.First(&class, classID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("class not found")
		}
		log.Printf("ERROR: UpdateClass: Failed fetch class %d by teacher %d: %v", classID, teacherID, err)
		return nil, errors.New("failed to retrieve class data")
	}
	if class.TeacherID == nil || *class.TeacherID != teacherID {
		return nil, errors.New("authorization error: you are not the teacher of this class")
	}

	updates := make(map[string]interface{})
	updated := false
	if name, ok := params.Args["name"].(string); ok && name != "" && class.Name != name {
		updates["Name"] = name
		updated = true
	}
	if subject, ok := params.Args["subject"].(string); ok && subject != "" && class.Subject != subject {
		updates["Subject"] = subject
		updated = true
	}
	if status, ok := params.Args["status"].(bool); ok && class.Status != status {
		updates["Status"] = status
		updated = true
	}
	if leaderIDInput, ok := params.Args["leaderID"].(int); ok {
		var newLeaderIDPtr *uint
		if leaderIDInput > 0 {
			newLeaderID := uint(leaderIDInput)
			var potentialLeader models.User
			if err := DB.Select("id", "role").First(&potentialLeader, newLeaderID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, fmt.Errorf("invalid leaderID: user %d not found", newLeaderID)
				}
				log.Printf("ERROR: UpdateClass: DB error check leader %d class %d: %v", newLeaderID, classID, err)
				return nil, errors.New("database error checking leader")
			}
			if potentialLeader.Role {
				return nil, fmt.Errorf("invalid leaderID: user %d is a teacher", newLeaderID)
			}
			newLeaderIDPtr = &newLeaderID
		}
		if (class.LeaderID == nil && newLeaderIDPtr != nil) || (class.LeaderID != nil && newLeaderIDPtr == nil) || (class.LeaderID != nil && newLeaderIDPtr != nil && *class.LeaderID != *newLeaderIDPtr) {
			updates["LeaderID"] = newLeaderIDPtr
			updated = true
		}
	}

	if updated {
		updates["UpdatedAt"] = time.Now()
		if err := DB.Model(&class).Updates(updates).Error; err != nil {
			log.Printf("ERROR: UpdateClass: Failed for class %d by teacher %d: %v", classID, teacherID, err)
			return nil, errors.New("failed to update class")
		}
		log.Printf("INFO: UpdateClass: Class %d updated by teacher %d", classID, teacherID)
	} else {
		log.Printf("INFO: UpdateClass: No changes for class %d.", classID)
	}

	var updatedClass models.Class
	err = DB.Preload("Teacher", func(db *gorm.DB) *gorm.DB { return db.Select("id", "fullname", "email", "role") }).
		Preload("Leader", func(db *gorm.DB) *gorm.DB { return db.Select("id", "fullname", "email", "role") }).
		First(&updatedClass, class.ID).Error
	if err != nil {
		log.Printf("WARNING: UpdateClass: Failed preload associations class %d: %v", class.ID, err)
		return class, nil
	}
	return updatedClass, nil
}

func DeleteClass(params graphql.ResolveParams) (interface{}, error) {
	teacherIDStr, err := requireTeacher(params)
	if err != nil {
		return nil, err
	}
	teacherID, err := getUserIDUint(teacherIDStr)
	if err != nil {
		return nil, err
	}
	classIDInput, ok := params.Args["classID"].(int)
	if !ok || classIDInput <= 0 {
		return nil, errors.New("invalid classID provided")
	}
	classID := uint(classIDInput)

	err = DB.Transaction(func(tx *gorm.DB) error {
		var class models.Class
		if err := tx.Select("id", "teacher_id").First(&class, classID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("class not found")
			}
			log.Printf("ERROR: Tx(DeleteClass): Failed fetch class %d by teacher %d: %v", classID, teacherID, err)
			return errors.New("failed to retrieve class data")
		}
		if class.TeacherID == nil || *class.TeacherID != teacherID {
			return errors.New("authorization error: you are not the teacher of this class")
		}
		var count int64
		if err := tx.Model(&models.StudentClass{}).Where("class_id = ? AND left_at IS NULL", classID).Count(&count).Error; err != nil {
			log.Printf("ERROR: Tx(DeleteClass): Failed count students class %d: %v", classID, err)
			return errors.New("failed to check student count")
		}
		if count >= 5 {
			return fmt.Errorf("cannot delete class: has %d active students (requires < 5)", count)
		}
		if err := tx.Where("class_id = ?", classID).Delete(&models.StudentClass{}).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("ERROR: Tx(DeleteClass): Failed delete enrollments class %d: %v", classID, err)
			return errors.New("failed to remove student enrollments")
		}
		result := tx.Delete(&models.Class{}, classID)
		if result.Error != nil {
			log.Printf("ERROR: Tx(DeleteClass): Failed delete class %d by teacher %d: %v", classID, teacherID, result.Error)
			return errors.New("failed to delete class")
		}
		if result.RowsAffected == 0 {
			log.Printf("WARN: Tx(DeleteClass): Delete class %d reported 0 rows affected.", classID)
			return errors.New("failed to delete class (not found during delete step)")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	log.Printf("INFO: DeleteClass: Class %d deleted by teacher %d", classID, teacherID)
	return map[string]interface{}{"message": fmt.Sprintf("Class %d deleted successfully", classID), "classID": classID}, nil
}

func JoinClass(params graphql.ResolveParams) (interface{}, error) {
	studentIDStr, err := requireStudent(params)
	if err != nil {
		return nil, err
	}
	studentID, err := getUserIDUint(studentIDStr)
	if err != nil {
		return nil, err
	}
	classIDInput, ok := params.Args["classID"].(int)
	if !ok || classIDInput <= 0 {
		return nil, errors.New("invalid classID provided")
	}
	classID := uint(classIDInput)

	var resultingEnrollment models.StudentClass
	err = DB.Transaction(func(tx *gorm.DB) error {
		var class models.Class
		if err := tx.Select("id", "status").First(&class, classID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("class not found")
			}
			log.Printf("ERROR: Tx(JoinClass): Failed fetch class %d for student %d: %v", classID, studentID, err)
			return errors.New("failed to retrieve class data")
		}
		if !class.Status {
			return errors.New("cannot join class: the class is not open")
		}

		var existingEnrollment models.StudentClass
		err = tx.Where("student_id = ? AND class_id = ?", studentID, classID).First(&existingEnrollment).Error
		if err == nil {
			if existingEnrollment.LeftAt == nil {
				return errors.New("student already enrolled in this class")
			}
			now := time.Now()
			updateData := map[string]interface{}{"left_at": nil, "enrolled_at": now}
			if err := tx.Model(&existingEnrollment).Updates(updateData).Error; err != nil {
				log.Printf("ERROR: Tx(JoinClass): Failed rejoin student %d class %d: %v", studentID, classID, err)
				return errors.New("failed to rejoin class")
			}
			resultingEnrollment = existingEnrollment
			log.Printf("INFO: Tx(JoinClass): Student %d rejoined class %d", studentID, classID)
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("ERROR: Tx(JoinClass): DB error check enrollment student %d, class %d: %v", studentID, classID, err)
			return errors.New("database error checking enrollment")
		}

		newEnrollment := models.StudentClass{StudentID: studentID, ClassID: classID, EnrolledAt: time.Now(), LeftAt: nil}
		if err := tx.Create(&newEnrollment).Error; err != nil {
			log.Printf("ERROR: Tx(JoinClass): Failed create enrollment student %d, class %d: %v", studentID, classID, err)
			return errors.New("failed to join class")
		}
		resultingEnrollment = newEnrollment
		log.Printf("INFO: Tx(JoinClass): Student %d joined class %d", studentID, classID)
		return nil
	})
	if err != nil {
		return nil, err
	}

	var finalEnrollmentWithStudent models.StudentClass
	err = DB.Preload("Student", func(db *gorm.DB) *gorm.DB { return db.Select("id", "fullname", "email", "role") }).
		First(&finalEnrollmentWithStudent, "student_id = ? AND class_id = ?", resultingEnrollment.StudentID, resultingEnrollment.ClassID).Error
	if err != nil {
		log.Printf("WARNING: JoinClass: Failed preload student info class %d: %v", classID, err)
		return resultingEnrollment, nil
	}
	return finalEnrollmentWithStudent, nil
}

func LeaveClass(params graphql.ResolveParams) (interface{}, error) {
	studentIDStr, err := requireStudent(params)
	if err != nil {
		return nil, err
	}
	studentID, err := getUserIDUint(studentIDStr)
	if err != nil {
		return nil, err
	}
	classIDInput, ok := params.Args["classID"].(int)
	if !ok || classIDInput <= 0 {
		return nil, errors.New("invalid classID provided")
	}
	classID := uint(classIDInput)

	var enrollment models.StudentClass
	err = DB.Where("student_id = ? AND class_id = ? AND left_at IS NULL", studentID, classID).First(&enrollment).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("student not currently enrolled in this class")
		}
		log.Printf("ERROR: LeaveClass: Failed fetch enrollment student %d, class %d: %v", studentID, classID, err)
		return nil, errors.New("database error finding enrollment")
	}

	now := time.Now()
	result := DB.Model(&enrollment).Update("left_at", &now)
	if result.Error != nil {
		log.Printf("ERROR: LeaveClass: Failed update left_at student %d, class %d: %v", studentID, classID, result.Error)
		return nil, errors.New("failed to leave class")
	}
	if result.RowsAffected == 0 {
		log.Printf("WARN: LeaveClass: No rows affected student %d, class %d (already left?)", studentID, classID)
		return map[string]interface{}{"message": fmt.Sprintf("No active enrollment found to update for student %d in class %d", studentID, classID), "classID": classID}, nil
	}
	log.Printf("INFO: LeaveClass: Student %d left class %d", studentID, classID)
	return map[string]interface{}{"message": fmt.Sprintf("Successfully left class %d", classID), "classID": classID}, nil
}

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
	"gorm.io/gorm/clause"
)

var DB *gorm.DB

func requireRole(params graphql.ResolveParams, requiredRole string) (string, error) {
	userIDStr, ok := middleware.GetUserIDFromContext(params.Context)
	if !ok {
		return "", errors.New("authorization required: user ID not found in context")
	}
	actualRole, ok := middleware.GetRoleFromContext(params.Context)
	if !ok {
		log.Printf("CRITICAL: requireRole: Role not found in context for userID %s", userIDStr)
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

func requireAuth(params graphql.ResolveParams) (userIDStr string, role string, err error) {
	userIDStr, ok := middleware.GetUserIDFromContext(params.Context)
	if !ok {
		err = errors.New("authorization required: user ID not found in context")
		return
	}
	role, ok = middleware.GetRoleFromContext(params.Context)
	if !ok {
		log.Printf("CRITICAL: requireAuth: Role not found in context for userID %s", userIDStr)
		err = errors.New("internal authorization error: role missing from context")
		return
	}
	return
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

func findUserByID(db *gorm.DB, userID uint, selectFields ...string) (models.User, error) {
	var user models.User
	query := db
	if len(selectFields) > 0 {
		query = query.Select(selectFields)
	}
	err := query.First(&user, userID).Error
	return user, err
}

func findClassByID(tx *gorm.DB, classID uint, lock bool) (models.Class, error) {
	var class models.Class
	query := tx
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.First(&class, classID).Error
	return class, err
}

func validateLeader(tx *gorm.DB, leaderID uint) error {
	var potentialLeader models.User
	err := tx.Select("id", "role").First(&potentialLeader, leaderID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("invalid leaderID: user %d not found", leaderID)
		}
		log.Printf("ERROR: validateLeader: DB error checking leader %d: %v", leaderID, err)
		return errors.New("database error checking leader")
	}
	if potentialLeader.Role {
		return fmt.Errorf("invalid leaderID: user %d is a teacher, not a student", leaderID)
	}
	return nil
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

	user, err := findUserByID(DB, userID, "id", "fullname", "email", "role", "created_at", "updated_at")
	if err != nil {
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
	err = DB.Select("id", "fullname", "email", "role", "created_at", "updated_at").Find(&users).Error
	if err != nil {
		log.Printf("ERROR: GetUsers: Failed: %v", err)
		return nil, errors.New("failed to retrieve users")
	}
	return users, nil
}

func GetClasses(params graphql.ResolveParams) (interface{}, error) {
	var classes []models.Class
	name, _ := params.Args["name"].(string)
	leaderName, _ := params.Args["leaderName"].(string)
	status, hasStatus := params.Args["status"].(bool)

	query := DB.Preload("Teacher", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).Preload("Leader", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	})

	if name != "" {
		query = query.Where("LOWER(name) LIKE LOWER(?)", "%"+name+"%")
	}
	if hasStatus {
		query = query.Where("status = ?", status)
	}
	if leaderName != "" {
		query = query.Joins("LEFT JOIN users AS leaders ON classes.leader_id = leaders.id").
			Where("LOWER(leaders.fullname) LIKE LOWER(?)", "%"+leaderName+"%")
	}

	if err := query.Find(&classes).Error; err != nil {
		log.Printf("ERROR: GetClasses: Failed to get classes: %v", err)
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
		log.Printf("ERROR: GetOpenClasses: Failed to retrieve open classes: %v", err)
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
	name, _ := params.Args["name"].(string)
	teacherName, _ := params.Args["teacherName"].(string)

	query := DB.Table("classes").Select("classes.*")
	query = query.Joins("JOIN student_classes sc ON classes.id = sc.class_id AND sc.left_at IS NULL").
		Where("sc.student_id = ?", studentID)

	if name != "" {
		query = query.Where("LOWER(classes.name) LIKE LOWER(?)", "%"+name+"%")
	}
	if teacherName != "" {
		query = query.Joins("JOIN users teacher ON classes.teacher_id = teacher.id").
			Where("LOWER(teacher.fullname) LIKE LOWER(?)", "%"+teacherName+"%")
	}

	query = query.Preload("Teacher", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).Preload("Leader", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	})

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
		Preload("Class.Teacher", func(db *gorm.DB) *gorm.DB { return db.Select("id", "fullname", "email", "role") }).
		Preload("Class.Leader", func(db *gorm.DB) *gorm.DB { return db.Select("id", "fullname", "email", "role") }).
		Preload("Class").
		First(&studentClass).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("class not found or student not actively enrolled in this class")
		}
		log.Printf("ERROR: GetStudentClassDetail: Failed fetch enrollment student %d, class %d: %v", studentID, classID, err)
		return nil, errors.New("failed to retrieve class details")
	}

	if studentClass.Class.ID == 0 {
		log.Printf("ERROR: GetStudentClassDetail: Preloaded Class data missing for StudentClass (StudentID: %d, ClassID: %d)", studentID, classID)
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
	err = DB.Preload("Teacher").
		Preload("Leader").
		Preload("StudentClasses", "left_at IS NULL").
		Preload("StudentClasses.Student", func(db *gorm.DB) *gorm.DB {
			return db.Select("id", "fullname", "email", "role")
		}).First(&class, classID).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("class not found")
		}
		log.Printf("ERROR: GetClassDetail: Failed fetch class %d by teacher %d: %v", classID, teacherID, err)
		return nil, errors.New("failed to retrieve class details")
	}

	if class.TeacherID == nil || *class.TeacherID != teacherID {
		log.Printf("WARN: GetClassDetail: Teacher %d attempted access to class %d owned by TeacherID %v", teacherID, classID, class.TeacherID)
		return nil, errors.New("authorization error: you are not the teacher of this class")
	}

	return class, nil
}

func RegisterUser(params graphql.ResolveParams) (interface{}, error) {
	fullname, _ := params.Args["fullname"].(string)
	email, _ := params.Args["email"].(string)
	password, _ := params.Args["password"].(string)
	roleInput, _ := params.Args["role"].(bool)

	if fullname == "" || email == "" || password == "" {
		return nil, errors.New("fullname, email, and password are required")
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		var existingUser models.User
		err := tx.Where("email = ?", email).First(&existingUser).Error
		if err == nil {
			return errors.New("email already exists")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("ERROR: RegisterUser TX: DB error check email %s: %v", email, err)
			return errors.New("database error checking email")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	hashedPasswordBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("ERROR: RegisterUser: Failed hash password for %s: %v", email, err)
		return nil, errors.New("failed to process password")
	}

	user := models.User{
		Fullname: fullname,
		Email:    email,
		Password: string(hashedPasswordBytes),
		Role:     roleInput,
	}

	if err := DB.Create(&user).Error; err != nil {
		log.Printf("ERROR: RegisterUser: Failed create user %s: %v", email, err)
		return nil, errors.New("failed to register user")
	}

	log.Printf("INFO: RegisterUser: User registered: %s (ID: %d)", user.Email, user.ID)

	userResult := map[string]interface{}{
		"id":         user.ID,
		"fullname":   user.Fullname,
		"email":      user.Email,
		"role":       user.Role,
		"created_at": user.CreatedAt,
		"updated_at": user.UpdatedAt,
	}
	return userResult, nil
}

func LoginUser(params graphql.ResolveParams) (interface{}, error) {
	email, _ := params.Args["email"].(string)
	password, _ := params.Args["password"].(string)

	if email == "" || password == "" {
		return nil, errors.New("email and password are required")
	}

	var user models.User
	err := DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("invalid email or password")
		}
		log.Printf("ERROR: LoginUser: DB error find user %s: %v", email, err)
		return nil, errors.New("database error during login")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, errors.New("invalid email or password")
	}

	if config.AppConfig == nil || config.AppConfig.HasuraJWTKey == "" || config.AppConfig.HasuraJWTType != "HS256" {
		log.Println("CRITICAL: LoginUser: Hasura JWT Key (HS256) not configured.")
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
		log.Printf("ERROR: LoginUser: Failed sign Hasura JWT for user %d: %v", user.ID, err)
		return nil, errors.New("failed to generate authentication token")
	}

	log.Printf("INFO: LoginUser: User logged in: %s (ID: %d)", user.Email, user.ID)

	userResult := map[string]interface{}{
		"id":         user.ID,
		"fullname":   user.Fullname,
		"email":      user.Email,
		"role":       user.Role,
		"created_at": user.CreatedAt,
		"updated_at": user.UpdatedAt,
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
	if fullnameOk && fullname == "" {
		return nil, errors.New("fullname cannot be empty")
	}
	if passwordOk && password == "" {
		return nil, errors.New("password cannot be empty")
	}

	user, err := findUserByID(DB, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("WARNING: UpdateUser: User %d (from token ID %s) not found in DB", userID, userIDStr)
			return nil, errors.New("user not found")
		}
		log.Printf("ERROR: UpdateUser: Failed fetch user %d: %v", userID, err)
		return nil, errors.New("failed to retrieve user data")
	}

	updates := make(map[string]interface{})
	needsDBUpdate := false

	if fullnameOk && user.Fullname != fullname {
		updates["Fullname"] = fullname
		needsDBUpdate = true
	}

	if passwordOk {
		hashedPasswordBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("ERROR: UpdateUser: Failed hash new password for user %d: %v", userID, err)
			return nil, errors.New("failed to process new password")
		}
		updates["Password"] = string(hashedPasswordBytes)
		needsDBUpdate = true
	}

	if needsDBUpdate {
		updates["UpdatedAt"] = time.Now()
		if err := DB.Model(&user).Updates(updates).Error; err != nil {
			log.Printf("ERROR: UpdateUser: Failed update user %d: %v", userID, err)
			return nil, errors.New("failed to update user information")
		}
		log.Printf("INFO: UpdateUser: User %d information updated.", userID)
		user, err = findUserByID(DB, userID, "id", "fullname", "email", "role", "created_at", "updated_at")
		if err != nil {
			log.Printf("WARN: UpdateUser: Failed to reload user %d after update: %v", userID, err)
		}
	} else {
		log.Printf("INFO: UpdateUser: No changes detected for user %d.", userID)
	}

	userResult := map[string]interface{}{
		"id":         user.ID,
		"fullname":   user.Fullname,
		"email":      user.Email,
		"role":       user.Role,
		"created_at": user.CreatedAt,
		"updated_at": user.UpdatedAt,
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
	if !statusProvided {
		statusInput = true
	}
	leaderIDInput, hasLeaderID := params.Args["leaderID"].(int)
	var leaderIDPtr *uint

	if name == "" || subject == "" {
		return nil, errors.New("class name and subject are required")
	}

	var createdClass models.Class

	err = DB.Transaction(func(tx *gorm.DB) error {
		if hasLeaderID && leaderIDInput > 0 {
			tempLeaderID := uint(leaderIDInput)
			if err := validateLeader(tx, tempLeaderID); err != nil {
				return err
			}
			leaderIDPtr = &tempLeaderID
		}

		classToCreate := models.Class{
			Name: name, Subject: subject, TeacherID: &teacherID, Status: statusInput, LeaderID: leaderIDPtr,
		}

		if err := tx.Create(&classToCreate).Error; err != nil {
			log.Printf("ERROR: CreateClass TX: Failed for '%s': %v", name, err)
			return errors.New("failed to create class")
		}
		log.Printf("INFO: CreateClass TX: Class '%s' (ID: %d) created.", classToCreate.Name, classToCreate.ID)

		if classToCreate.LeaderID != nil {
			enrollment := models.StudentClass{
				StudentID: *classToCreate.LeaderID, ClassID: classToCreate.ID, EnrolledAt: time.Now(),
			}
			if err := tx.Create(&enrollment).Error; err != nil {
				log.Printf("ERROR: CreateClass TX: Failed to auto-enroll leader %d for class %d: %v", *classToCreate.LeaderID, classToCreate.ID, err)
				return errors.New("failed to enroll class leader")
			}
			log.Printf("INFO: CreateClass TX: Auto-enrolled leader %d for class %d", *classToCreate.LeaderID, classToCreate.ID)
		}

		createdClass = classToCreate
		return nil
	})

	if err != nil {
		return nil, err
	}

	var finalClassResult models.Class
	loadErr := DB.Preload("Teacher").Preload("Leader").First(&finalClassResult, createdClass.ID).Error
	if loadErr != nil {
		log.Printf("WARNING: CreateClass: Failed to reload created class %d with associations: %v", createdClass.ID, loadErr)
		return createdClass, nil
	}

	log.Printf("INFO: CreateClass: Success for class ID %d", finalClassResult.ID)
	return finalClassResult, nil
}

func processClassUpdateArgs(tx *gorm.DB, params graphql.ResolveParams, currentClass *models.Class) (updates map[string]interface{}, newLeaderIDPtr *uint, leaderChanged bool, needsDBUpdate bool, err error) {
	updates = make(map[string]interface{})
	oldLeaderIDPtr := currentClass.LeaderID

	if name, ok := params.Args["name"].(string); ok && name != "" && currentClass.Name != name {
		updates["Name"] = name
		needsDBUpdate = true
	}
	if subject, ok := params.Args["subject"].(string); ok && subject != "" && currentClass.Subject != subject {
		updates["Subject"] = subject
		needsDBUpdate = true
	}
	if status, ok := params.Args["status"].(bool); ok && currentClass.Status != status {
		updates["Status"] = status
		needsDBUpdate = true
	}

	rawLeaderID, leaderIDProvided := params.Args["leaderID"]
	if leaderIDProvided {
		leaderIDInput, isInt := rawLeaderID.(int)
		if !isInt || leaderIDInput <= 0 {
			newLeaderIDPtr = nil
		} else {
			tempLeaderID := uint(leaderIDInput)
			if err = validateLeader(tx, tempLeaderID); err != nil {
				return
			}
			newLeaderIDPtr = &tempLeaderID
		}

		if (oldLeaderIDPtr == nil && newLeaderIDPtr != nil) ||
			(oldLeaderIDPtr != nil && newLeaderIDPtr == nil) ||
			(oldLeaderIDPtr != nil && newLeaderIDPtr != nil && *oldLeaderIDPtr != *newLeaderIDPtr) {
			updates["LeaderID"] = newLeaderIDPtr
			needsDBUpdate = true
			leaderChanged = true
		}
	} else {
		newLeaderIDPtr = oldLeaderIDPtr
	}

	return
}

func handleLeaderEnrollmentChange(tx *gorm.DB, classID uint, oldLeaderIDPtr *uint, newLeaderIDPtr *uint) error {
	now := time.Now()
	logPrefix := fmt.Sprintf("handleLeaderEnrollmentChange (Class %d):", classID)
	if newLeaderIDPtr != nil {
		newLeaderID := *newLeaderIDPtr
		log.Printf("%s New leader is %d. Ensuring enrollment.", logPrefix, newLeaderID)

		var existingEnrollment models.StudentClass
		errCheck := tx.Where("student_id = ? AND class_id = ?", newLeaderID, classID).First(&existingEnrollment).Error

		if errors.Is(errCheck, gorm.ErrRecordNotFound) {
			enrollment := models.StudentClass{StudentID: newLeaderID, ClassID: classID, EnrolledAt: now, LeftAt: nil}
			if err := tx.Create(&enrollment).Error; err != nil {
				log.Printf("ERROR: %s Failed to create enrollment for new leader %d: %v", logPrefix, newLeaderID, err)
				return errors.New("failed to enroll new class leader")
			}
			log.Printf("INFO: %s Enrolled new leader %d.", logPrefix, newLeaderID)
		} else if errCheck == nil {
			if existingEnrollment.LeftAt != nil {
				if err := tx.Model(&existingEnrollment).Update("left_at", nil).Error; err != nil {
					log.Printf("ERROR: %s Failed to re-enroll new leader %d: %v", logPrefix, newLeaderID, err)
					return errors.New("failed to re-enroll new class leader")
				}
				log.Printf("INFO: %s Re-enrolled new leader %d.", logPrefix, newLeaderID)
			} else {
				log.Printf("INFO: %s New leader %d already actively enrolled.", logPrefix, newLeaderID)
			}
		} else {
			log.Printf("ERROR: %s DB error checking enrollment for new leader %d: %v", logPrefix, newLeaderID, errCheck)
			return errors.New("database error checking new leader enrollment")
		}
	} else {
		log.Printf("%s Leader removed. Old leader enrollment status unchanged.", logPrefix)
	}

	return nil
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

	var finalClassResult models.Class

	err = DB.Transaction(func(tx *gorm.DB) error {
		class, err := findClassByID(tx, classID, true)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("class not found")
			}
			log.Printf("ERROR: UpdateClass TX: Failed fetch/lock class %d: %v", classID, err)
			return errors.New("failed to retrieve class data")
		}

		if class.TeacherID == nil || *class.TeacherID != teacherID {
			return errors.New("authorization error: you are not the teacher of this class")
		}

		oldLeaderIDPtr := class.LeaderID

		updates, newLeaderIDPtr, leaderChanged, needsDBUpdate, err := processClassUpdateArgs(tx, params, &class)
		if err != nil {
			return err
		}

		if needsDBUpdate {
			updates["UpdatedAt"] = time.Now()
			if err := tx.Model(&class).Updates(updates).Error; err != nil {
				log.Printf("ERROR: UpdateClass TX: Failed DB update for class %d: %v", classID, err)
				return errors.New("failed to update class information")
			}
			log.Printf("INFO: UpdateClass TX: Updated fields for class %d", classID)
		}
		if leaderChanged {
			if err := handleLeaderEnrollmentChange(tx, classID, oldLeaderIDPtr, newLeaderIDPtr); err != nil {
				return err
			}
		}
		loadErr := tx.Preload("Teacher").Preload("Leader").First(&finalClassResult, classID).Error
		if loadErr != nil {
			log.Printf("ERROR: UpdateClass TX: Failed to reload updated class %d: %v", classID, loadErr)
			return errors.New("failed to retrieve final class details after update")
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	log.Printf("INFO: UpdateClass: Successfully processed update request for class %d.", classID)
	return finalClassResult, nil
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
			log.Printf("ERROR: DeleteClass TX: Failed fetch class %d: %v", classID, err)
			return errors.New("failed to retrieve class data")
		}
		if class.TeacherID == nil || *class.TeacherID != teacherID {
			return errors.New("authorization error: you are not the teacher of this class")
		}

		var count int64
		if err := tx.Model(&models.StudentClass{}).Where("class_id = ? AND left_at IS NULL", classID).Count(&count).Error; err != nil {
			log.Printf("ERROR: DeleteClass TX: Failed count students class %d: %v", classID, err)
			return errors.New("failed to check student count")
		}
		if count >= 5 {
			return fmt.Errorf("cannot delete class: has %d active students (requires < 5)", count)
		}

		if err := tx.Where("class_id = ?", classID).Delete(&models.StudentClass{}).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				log.Printf("ERROR: DeleteClass TX: Failed delete enrollments class %d: %v", classID, err)
				return errors.New("failed to remove student enrollments")
			}
		}

		result := tx.Delete(&models.Class{}, classID)
		if result.Error != nil {
			log.Printf("ERROR: DeleteClass TX: Failed delete class %d: %v", classID, result.Error)
			return errors.New("failed to delete class")
		}
		if result.RowsAffected == 0 {
			log.Printf("WARN: DeleteClass TX: Delete class %d reported 0 rows affected.", classID)
			return errors.New("failed to delete class (not found during delete step)")
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	log.Printf("INFO: DeleteClass: Class %d deleted by teacher %d", classID, teacherID)
	return map[string]interface{}{
		"message": fmt.Sprintf("Class %d deleted successfully", classID),
		"classID": classID,
	}, nil
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

	var finalEnrollment models.StudentClass

	err = DB.Transaction(func(tx *gorm.DB) error {
		var class models.Class
		if err := tx.Select("id", "status").First(&class, classID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("class not found")
			}
			log.Printf("ERROR: JoinClass TX: Failed fetch class %d: %v", classID, err)
			return errors.New("failed to retrieve class data")
		}
		if !class.Status {
			return errors.New("cannot join class: the class is not open")
		}

		var existingEnrollment models.StudentClass
		errCheck := tx.Where("student_id = ? AND class_id = ?", studentID, classID).First(&existingEnrollment).Error

		now := time.Now()

		if errCheck == nil {
			if existingEnrollment.LeftAt == nil {
				return errors.New("student already actively enrolled in this class")
			} else {
				log.Printf("INFO: JoinClass TX: Student %d rejoining class %d.", studentID, classID)
				updateData := map[string]interface{}{"left_at": nil, "enrolled_at": now}
				if err := tx.Model(&existingEnrollment).Updates(updateData).Error; err != nil {
					log.Printf("ERROR: JoinClass TX: Failed rejoin student %d class %d: %v", studentID, classID, err)
					return errors.New("failed to rejoin class")
				}
				finalEnrollment = existingEnrollment
				finalEnrollment.LeftAt = nil
				finalEnrollment.EnrolledAt = now
			}
		} else if errors.Is(errCheck, gorm.ErrRecordNotFound) {
			log.Printf("INFO: JoinClass TX: Student %d joining class %d for the first time.", studentID, classID)
			newEnrollment := models.StudentClass{
				StudentID: studentID, ClassID: classID, EnrolledAt: now, LeftAt: nil,
			}
			if err := tx.Create(&newEnrollment).Error; err != nil {
				log.Printf("ERROR: JoinClass TX: Failed create enrollment student %d, class %d: %v", studentID, classID, err)
				return errors.New("failed to join class")
			}
			finalEnrollment = newEnrollment
		} else {
			log.Printf("ERROR: JoinClass TX: DB error check enrollment student %d, class %d: %v", studentID, classID, errCheck)
			return errors.New("database error checking enrollment")
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	var reloadedEnrollment models.StudentClass
	loadErr := DB.Preload("Student", func(db *gorm.DB) *gorm.DB { return db.Select("id", "fullname", "email", "role") }).
		Where("student_id = ? AND class_id = ?", finalEnrollment.StudentID, finalEnrollment.ClassID).
		First(&reloadedEnrollment).Error

	if loadErr != nil {
		log.Printf("WARNING: JoinClass: Failed preload student info for enrollment (Student %d, Class %d): %v", finalEnrollment.StudentID, classID, loadErr)
		return finalEnrollment, nil
	}

	log.Printf("INFO: JoinClass: Student %d successfully joined/rejoined class %d", studentID, classID)
	return reloadedEnrollment, nil
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

	now := time.Now()
	var rowsAffected int64

	err = DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.StudentClass{}).
			Where("student_id = ? AND class_id = ? AND left_at IS NULL", studentID, classID).
			Update("left_at", &now)

		if result.Error != nil {
			log.Printf("ERROR: LeaveClass TX: Failed update left_at student %d, class %d: %v", studentID, classID, result.Error)
			return errors.New("failed to update enrollment status")
		}

		rowsAffected = result.RowsAffected

		if rowsAffected == 0 {
			log.Printf("WARN: LeaveClass TX: No active enrollment found for student %d, class %d.", studentID, classID)
			var count int64
			tx.Model(&models.StudentClass{}).Where("student_id = ? AND class_id = ?", studentID, classID).Count(&count)
			if count > 0 {
				return errors.New("student has already left this class")
			} else {
				return errors.New("student not enrolled in this class")
			}
		}

		var classInfo models.Class

		if err := tx.Select("leader_id").First(&classInfo, classID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				log.Printf("ERROR: LeaveClass TX: Class %d not found after updating enrollment for student %d", classID, studentID)
				return errors.New("internal error: class not found during leave process")
			}
			log.Printf("ERROR: LeaveClass TX: Failed to check leader for class %d: %v", classID, err)
			return errors.New("failed to check class leader status")
		}

		if classInfo.LeaderID != nil && *classInfo.LeaderID == studentID {
			log.Printf("INFO: LeaveClass TX: Student %d was the leader of class %d. Removing leader.", studentID, classID)
			if err := tx.Model(&models.Class{}).Where("id = ?", classID).Update("leader_id", nil).Error; err != nil {
				log.Printf("ERROR: LeaveClass TX: Failed to remove leader %d from class %d: %v", studentID, classID, err)
				return errors.New("failed to update class leader")
			}
			log.Printf("INFO: LeaveClass TX: Removed leader %d from class %d.", studentID, classID)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	log.Printf("INFO: LeaveClass: Student %d successfully left class %d (and removed as leader if applicable).", studentID, classID)
	return map[string]interface{}{
		"message": fmt.Sprintf("Successfully left class %d", classID),
		"classID": classID,
	}, nil
}

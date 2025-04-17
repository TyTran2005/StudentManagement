package resolvers

import (
	"errors"
	"fmt"
	"log"
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

func requireTeacher(params graphql.ResolveParams) (uint, error) {
	userID, ok := middleware.GetUserIDFromContext(params.Context)
	if !ok {
		return 0, errors.New("authorization required: please log in")
	}
	role, ok := middleware.GetRoleFromContext(params.Context)
	if !ok {
		log.Printf("ERROR: Role not found in context for userID %d", userID)
		return 0, errors.New("internal authorization error: role missing")
	}
	if !role {
		return 0, errors.New("authorization error: teacher access required")
	}
	return userID, nil
}

func requireStudent(params graphql.ResolveParams) (uint, error) {
	userID, ok := middleware.GetUserIDFromContext(params.Context)
	if !ok {
		return 0, errors.New("authorization required: please log in")
	}
	role, ok := middleware.GetRoleFromContext(params.Context)
	if !ok {
		log.Printf("ERROR: Role not found in context for userID %d", userID)
		return 0, errors.New("internal authorization error: role missing")
	}
	if role {
		return 0, errors.New("authorization error: student access required")
	}
	return userID, nil
}

func requireAuth(params graphql.ResolveParams) (uint, bool, error) {
	userID, ok := middleware.GetUserIDFromContext(params.Context)
	if !ok {
		return 0, false, errors.New("authorization required: please log in")
	}
	role, ok := middleware.GetRoleFromContext(params.Context)
	if !ok {
		log.Printf("ERROR: Role not found in context for userID %d", userID)
		return 0, false, errors.New("internal authorization error: role missing")
	}
	return userID, role, nil
}

func GetCurrentUser(params graphql.ResolveParams) (interface{}, error) {
	userID, _, err := requireAuth(params)
	if err != nil {
		return nil, err
	}

	var user models.User
	if err := DB.First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("WARNING: User %d from token not found in DB", userID)
			return nil, errors.New("user not found")
		}
		log.Printf("ERROR: Failed to fetch user %d: %v", userID, err)
		return nil, errors.New("failed to retrieve user data")
	}
	user.Password = ""
	return user, nil
}

func GetUsers(params graphql.ResolveParams) (interface{}, error) {
	_, err := requireTeacher(params)
	if err != nil {
		return nil, err
	}

	var users []models.User
	if err := DB.Select("id", "fullname", "email", "role", "created_at", "updated_at").Find(&users).Error; err != nil {
		log.Printf("ERROR: Failed to get users: %v", err)
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
		query = query.Joins("LEFT JOIN users AS leader ON classes.leader_id = leader.id").
			Where("LOWER(leader.fullname) LIKE LOWER(?)", "%"+leaderName+"%")
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
	studentID, err := requireStudent(params)
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
		log.Printf("ERROR: Failed to get registered classes for student %d: %v", studentID, err)
		return nil, errors.New("failed to retrieve registered classes")
	}

	return classes, nil
}

func GetStudentClassDetail(params graphql.ResolveParams) (interface{}, error) {
	studentID, err := requireStudent(params)
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
		First(&studentClass).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("class not found or student not enrolled in this class")
		}
		log.Printf("ERROR: Failed to get student class detail for student %d, class %d: %v", studentID, classID, err)
		return nil, errors.New("failed to retrieve class details")
	}

	return studentClass.Class, nil
}

func GetClassDetail(params graphql.ResolveParams) (interface{}, error) {
	teacherID, err := requireTeacher(params)
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
		log.Printf("ERROR: Failed to get class detail for class %d by teacher %d: %v", classID, teacherID, err)
		return nil, errors.New("failed to retrieve class details")
	}
	return class, nil
}

func RegisterUser(params graphql.ResolveParams) (interface{}, error) {
	fullname := params.Args["fullname"].(string)
	email := params.Args["email"].(string)
	password := params.Args["password"].(string)
	role := params.Args["role"].(bool)

	if fullname == "" || email == "" || password == "" {
		return nil, errors.New("fullname, email, and password are required")
	}

	var existingUser models.User
	err := DB.Where("email = ?", email).First(&existingUser).Error
	if err == nil {
		return nil, errors.New("email already exists")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("ERROR: DB error checking existing email %s: %v", email, err)
		return nil, errors.New("database error checking email")
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("ERROR: Failed to hash password for email %s: %v", email, err)
		return nil, errors.New("failed to process password")
	}

	user := models.User{
		Fullname: fullname,
		Email:    email,
		Password: string(hashedPassword),
		Role:     role,
	}

	if err := DB.Create(&user).Error; err != nil {
		log.Printf("ERROR: Failed to create user %s: %v", email, err)
		return nil, errors.New("failed to register user")
	}

	user.Password = ""
	return user, nil
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
		log.Printf("ERROR: DB error finding user %s for login: %v", email, err)
		return nil, errors.New("database error during login")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, errors.New("invalid email or password")
	}
	if config.AppConfig == nil || config.AppConfig.JWTSecretKey == "" {
		log.Println("ERROR: JWT Secret Key is not configured for token generation.")
		return nil, errors.New("internal server error: JWT configuration missing")
	}
	jwtSecret := []byte(config.AppConfig.JWTSecretKey)
	expirationTime := time.Now().Add(config.AppConfig.JWTExpiresHour)

	claims := jwt.MapClaims{
		"userID": user.ID,
		"role":   user.Role,
		"exp":    expirationTime.Unix(),
		"iat":    time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		log.Printf("ERROR: Failed to sign JWT token for user %d: %v", user.ID, err)
		return nil, errors.New("failed to generate authentication token")
	}

	user.Password = ""

	return map[string]interface{}{
		"token": tokenString,
		"user":  user,
	}, nil
}

func UpdateUser(params graphql.ResolveParams) (interface{}, error) {
	userID, _, err := requireAuth(params)
	if err != nil {
		return nil, err
	}

	fullname, fullnameOk := params.Args["fullname"].(string)
	password, passwordOk := params.Args["password"].(string)

	if !fullnameOk && !passwordOk {
		return nil, errors.New("no fields provided for update")
	}

	var user models.User
	if err := DB.First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("WARNING: User %d from token not found in DB for update", userID)
			return nil, errors.New("user not found")
		}
		log.Printf("ERROR: Failed to fetch user %d for update: %v", userID, err)
		return nil, errors.New("failed to retrieve user data")
	}

	updated := false
	if fullnameOk && fullname != "" && user.Fullname != fullname {
		user.Fullname = fullname
		updated = true
	}

	if passwordOk && password != "" {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("ERROR: Failed to hash new password for user %d: %v", userID, err)
			return nil, errors.New("failed to process new password")
		}
		if string(hashedPassword) != user.Password {
			user.Password = string(hashedPassword)
			updated = true
		}
	}

	if updated {
		user.UpdatedAt = time.Now()
		if err := DB.Save(&user).Error; err != nil {
			log.Printf("ERROR: Failed to update user %d: %v", userID, err)
			return nil, errors.New("failed to update user information")
		}
	}
	user.Password = ""
	return user, nil
}

func CreateClass(params graphql.ResolveParams) (interface{}, error) {
	teacherID, err := requireTeacher(params)
	if err != nil {
		return nil, err
	}
	name, _ := params.Args["name"].(string)
	subject, _ := params.Args["subject"].(string)
	status, _ := params.Args["status"].(bool)
	leaderIDInput, hasLeaderID := params.Args["leaderID"].(int)

	if name == "" || subject == "" {
		return nil, errors.New("class name and subject are required")
	}

	class := models.Class{
		Name:      name,
		Subject:   subject,
		TeacherID: &teacherID,
		Status:    status,
	}

	if hasLeaderID && leaderIDInput > 0 {
		leaderID := uint(leaderIDInput)
		var potentialLeader models.User
		if err := DB.Select("id", "role").First(&potentialLeader, leaderID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("invalid leaderID: user with ID %d not found", leaderID)
			}
			log.Printf("ERROR: DB error checking leader %d for class creation by teacher %d: %v", leaderID, teacherID, err)
			return nil, errors.New("database error checking leader")
		}
		if potentialLeader.Role {
			return nil, fmt.Errorf("invalid leaderID: user %d is a teacher, not a student", leaderID)
		}
		class.LeaderID = &leaderID
	}

	if err := DB.Create(&class).Error; err != nil {
		log.Printf("ERROR: Failed to create class '%s' by teacher %d: %v", name, teacherID, err)
		return nil, errors.New("failed to create class")
	}

	err = DB.Preload("Teacher", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).Preload("Leader", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).First(&class, class.ID).Error

	if err != nil {
		log.Printf("WARNING: Failed to preload teacher/leader after creating class %d: %v", class.ID, err)
	}

	return class, nil
}

func UpdateClass(params graphql.ResolveParams) (interface{}, error) {
	teacherID, err := requireTeacher(params)
	if err != nil {
		return nil, err
	}

	classIDInput, ok := params.Args["classID"].(int)
	if !ok || classIDInput <= 0 {
		return nil, errors.New("invalid classID provided")
	}
	classID := uint(classIDInput)

	name, isNameProvided := params.Args["name"].(string)
	subject, isSubjectProvided := params.Args["subject"].(string)
	status, isStatusProvided := params.Args["status"].(bool)
	leaderIDInput, isLeaderIDProvided := params.Args["leaderID"].(int)

	var class models.Class
	if err := DB.First(&class, classID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("class not found")
		}
		log.Printf("ERROR: Failed to fetch class %d for update by teacher %d: %v", classID, teacherID, err)
		return nil, errors.New("failed to retrieve class data")
	}

	if class.TeacherID == nil || *class.TeacherID != teacherID {
		return nil, errors.New("authorization error: you are not the teacher of this class")
	}

	updated := false
	if isNameProvided && name != "" && class.Name != name {
		class.Name = name
		updated = true
	}
	if isSubjectProvided && subject != "" && class.Subject != subject {
		class.Subject = subject
		updated = true
	}
	if isStatusProvided && class.Status != status {
		class.Status = status
		updated = true
	}

	if isLeaderIDProvided {
		var newLeaderIDPtr *uint
		if leaderIDInput > 0 {
			newLeaderID := uint(leaderIDInput)
			var potentialLeader models.User
			if err := DB.Select("id", "role").First(&potentialLeader, newLeaderID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, fmt.Errorf("invalid leaderID: user with ID %d not found", newLeaderID)
				}
				log.Printf("ERROR: DB error checking new leader %d for class update %d: %v", newLeaderID, classID, err)
				return nil, errors.New("database error checking leader")
			}
			if potentialLeader.Role {
				return nil, fmt.Errorf("invalid leaderID: user %d is a teacher", newLeaderID)
			}
			newLeaderIDPtr = &newLeaderID
		}

		if (class.LeaderID == nil && newLeaderIDPtr != nil) || (class.LeaderID != nil && newLeaderIDPtr == nil) || (class.LeaderID != nil && newLeaderIDPtr != nil && *class.LeaderID != *newLeaderIDPtr) {
			class.LeaderID = newLeaderIDPtr
			updated = true
		}
	}

	if updated {
		class.UpdatedAt = time.Now()
		if err := DB.Save(&class).Error; err != nil {
			log.Printf("ERROR: Failed to update class %d by teacher %d: %v", classID, teacherID, err)
			return nil, errors.New("failed to update class")
		}
	}

	err = DB.Preload("Teacher", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).Preload("Leader", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).First(&class, class.ID).Error

	if err != nil {
		log.Printf("WARNING: Failed to preload after updating class %d: %v", class.ID, err)
	}

	return class, nil
}

func DeleteClass(params graphql.ResolveParams) (interface{}, error) {
	teacherID, err := requireTeacher(params)
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
			log.Printf("ERROR: Failed to fetch class %d for delete by teacher %d: %v", classID, teacherID, err)
			return errors.New("failed to retrieve class data")
		}

		if class.TeacherID == nil || *class.TeacherID != teacherID {
			return errors.New("authorization error: you are not the teacher of this class")
		}

		var count int64
		if err := tx.Model(&models.StudentClass{}).Where("class_id = ? AND left_at IS NULL", classID).Count(&count).Error; err != nil {
			log.Printf("ERROR: Failed to count students for class %d delete: %v", classID, err)
			return errors.New("failed to count students in class")
		}

		if count >= 5 {
			return fmt.Errorf("cannot delete class: class has %d students (requires less than 5)", count)
		}

		if err := tx.Where("class_id = ?", classID).Delete(&models.StudentClass{}).Error; err != nil {
			log.Printf("ERROR: Failed to delete student enrollments for class %d: %v", classID, err)
			return errors.New("failed to remove students before deleting class")
		}

		if err := tx.Delete(&models.Class{}, classID).Error; err != nil {
			log.Printf("ERROR: Failed to delete class %d by teacher %d: %v", classID, teacherID, err)
			return errors.New("failed to delete class")
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	log.Printf("INFO: Class %d deleted successfully by teacher %d", classID, teacherID)
	return fmt.Sprintf("Class %d deleted successfully", classID), nil
}

func JoinClass(params graphql.ResolveParams) (interface{}, error) {
	studentID, err := requireStudent(params)
	if err != nil {
		return nil, err
	}

	classIDInput, ok := params.Args["classID"].(int)
	if !ok || classIDInput <= 0 {
		return nil, errors.New("invalid classID provided")
	}
	classID := uint(classIDInput)

	var studentClass models.StudentClass
	err = DB.Transaction(func(tx *gorm.DB) error {
		var class models.Class
		if err := tx.Select("id", "status").First(&class, classID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("class not found")
			}
			log.Printf("ERROR: Failed fetch class %d for student %d join: %v", classID, studentID, err)
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
			} else {
				existingEnrollment.LeftAt = nil
				existingEnrollment.EnrolledAt = time.Now()
				if err := tx.Save(&existingEnrollment).Error; err != nil {
					log.Printf("ERROR: Failed to rejoin student %d to class %d: %v", studentID, classID, err)
					return errors.New("failed to rejoin class")
				}
				studentClass = existingEnrollment
				log.Printf("INFO: Student %d rejoined class %d", studentID, classID)
				return nil
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("ERROR: DB error checking enrollment for student %d, class %d: %v", studentID, classID, err)
			return errors.New("database error checking enrollment")
		}

		newEnrollment := models.StudentClass{
			StudentID:  studentID,
			ClassID:    classID,
			EnrolledAt: time.Now(),
			LeftAt:     nil,
		}
		if err := tx.Create(&newEnrollment).Error; err != nil {
			log.Printf("ERROR: Failed to join student %d to class %d: %v", studentID, classID, err)
			return errors.New("failed to join class")
		}
		studentClass = newEnrollment
		log.Printf("INFO: Student %d joined class %d", studentID, classID)
		return nil
	})

	if err != nil {
		return nil, err
	}

	err = DB.Preload("Student", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "fullname", "email", "role")
	}).First(&studentClass, "student_id = ? AND class_id = ?", studentID, classID).Error

	if err != nil {
		log.Printf("WARNING: Failed to preload student info after joining class %d: %v", classID, err)
	}

	return studentClass, nil
}

func LeaveClass(params graphql.ResolveParams) (interface{}, error) {
	studentID, err := requireStudent(params)
	if err != nil {
		return nil, err
	}

	classIDInput, ok := params.Args["classID"].(int)
	if !ok || classIDInput <= 0 {
		return nil, errors.New("invalid classID provided")
	}
	classID := uint(classIDInput)

	// Tìm bản ghi enrollment của sinh viên trong lớp đó và chưa rời
	var enrollment models.StudentClass
	err = DB.Where("student_id = ? AND class_id = ? AND left_at IS NULL", studentID, classID).First(&enrollment).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("student not currently enrolled in this class")
		}
		log.Printf("ERROR: Failed fetch enrollment for student %d, class %d leave: %v", studentID, classID, err)
		return nil, errors.New("database error finding enrollment")
	}
	now := time.Now()
	result := DB.Model(&enrollment).Update("left_at", &now)

	if result.Error != nil {
		log.Printf("ERROR: Failed to update left_at for student %d, class %d: %v", studentID, classID, result.Error)
		return nil, errors.New("failed to leave class")
	}

	if result.RowsAffected == 0 {
		log.Printf("WARNING: No rows affected when student %d tried to leave class %d", studentID, classID)
		return nil, errors.New("failed to leave class (no record updated)")
	}

	log.Printf("INFO: Student %d left class %d", studentID, classID)
	return fmt.Sprintf("Successfully left class %d", classID), nil
}

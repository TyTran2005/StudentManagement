package resolvers

import (
	"errors"
	"student-management-api/internal/models"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/graphql-go/graphql"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var DB *gorm.DB

var jwtSecret = []byte("jsdfgslhfkfhsdjjsd")

func GetUsers() ([]models.User, error) {
	var users []models.User
	if err := DB.Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func RegisterUser(params graphql.ResolveParams) (interface{}, error) {
	fullname := params.Args["fullname"].(string)
	email := params.Args["email"].(string)
	password := params.Args["password"].(string)
	role := params.Args["role"].(bool)

	var existingUser models.User
	if err := DB.Where("email = ?", email).First(&existingUser).Error; err == nil {
		return nil, errors.New("email already exists")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("failed to hash password")
	}

	user := models.User{
		Fullname: fullname,
		Email:    email,
		Password: string(hashedPassword),
		Role:     role,
	}

	if err := DB.Create(&user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

func LoginUser(params graphql.ResolveParams) (interface{}, error) {
	email := params.Args["email"].(string)
	password := params.Args["password"].(string)
	var user models.User
	if err := DB.Where("email = ?", email).First(&user).Error; err != nil {
		return nil, errors.New("invalid email or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, errors.New("invalid email or password")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"userID": user.ID,
		"role":   user.Role,
		"exp":    time.Now().Add(time.Hour * 24).Unix(),
	})
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		return nil, errors.New("failed to generate token")
	}
	return map[string]interface{}{
		"token": tokenString,
		"user":  user,
	}, nil
}

func UpdateUser(params graphql.ResolveParams) (interface{}, error) {
	id := params.Args["id"].(int)
	fullname := params.Args["fullname"].(string)
	password := params.Args["password"].(string)

	var user models.User
	if err := DB.First(&user, id).Error; err != nil {
		return nil, errors.New("user not found")
	}

	user.Fullname = fullname
	if password != "" {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, errors.New("failed to hash password")
		}
		user.Password = string(hashedPassword)
	}

	if err := DB.Save(&user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

func CreateClass(params graphql.ResolveParams) (interface{}, error) {
	name := params.Args["name"].(string)
	subject := params.Args["subject"].(string)
	teacherIDInput := params.Args["teacherID"].(int)
	status := params.Args["status"].(bool)
	leaderIDInput := params.Args["leaderID"].(int)

	var teacher models.User
	if err := DB.First(&teacher, teacherIDInput).Error; err != nil {
		return nil, errors.New("teacher user not found")
	}
	if !teacher.Role {
		return nil, errors.New("the specified user is not a teacher")
	}

	teacherID := uint(teacherIDInput)
	leaderID := uint(leaderIDInput)
	class := models.Class{
		Name:      name,
		Subject:   subject,
		TeacherID: &teacherID,
		Status:    status,
		LeaderID:  &leaderID,
	}
	if err := DB.Create(&class).Error; err != nil {
		return nil, err
	}
	return class, nil
}

func JoinClass(params graphql.ResolveParams) (interface{}, error) {
	studentID := uint(params.Args["studentID"].(int))
	classID := uint(params.Args["classID"].(int))
	var class models.Class
	if err := DB.First(&class, classID).Error; err != nil {
		return nil, errors.New("class not found")
	}
	if !class.Status {
		return nil, errors.New("cannot join class: the class is not open")
	}
	var existingStudentClass models.StudentClass
	if err := DB.Where("student_id = ? AND class_id = ?", studentID, classID).First(&existingStudentClass).Error; err == nil {
		return nil, errors.New("student already joined this class")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	studentClass := models.StudentClass{
		StudentID: studentID,
		ClassID:   classID,
	}
	if err := DB.Create(&studentClass).Error; err != nil {
		return nil, err
	}
	return studentClass, nil
}

func DeleteClass(params graphql.ResolveParams) (interface{}, error) {
	userID := uint(params.Args["userID"].(int))
	var user models.User
	if err := DB.First(&user, userID).Error; err != nil {
		return nil, errors.New("user not found")
	}
	if !user.Role {
		return nil, errors.New("only teachers can delete classes")
	}
	classID := uint(params.Args["classID"].(int))
	var class models.Class
	if err := DB.First(&class, classID).Error; err != nil {
		return nil, errors.New("class not found")
	}
	if class.TeacherID == nil || *class.TeacherID != userID {
		return nil, errors.New("you are not the teacher of this class")
	}
	var count int64
	if err := DB.Model(&models.StudentClass{}).Where("class_id = ?", classID).Count(&count).Error; err != nil {
		return nil, errors.New("failed to count students in class")
	}
	if count >= 5 {
		return nil, errors.New("cannot delete class with 5 or more students")
	}
	if err := DB.Delete(&models.Class{}, classID).Error; err != nil {
		return nil, err
	}
	return map[string]string{"message": "class deleted successfully"}, nil
}

func LeaveClass(params graphql.ResolveParams) (interface{}, error) {
	studentID := uint(params.Args["studentID"].(int))
	classID := uint(params.Args["classID"].(int))

	if err := DB.Where("student_id = ? AND class_id = ?", studentID, classID).Delete(&models.StudentClass{}).Error; err != nil {
		return nil, err
	}
	return map[string]string{"message": "left class successfully"}, nil
}

func GetClasses(params graphql.ResolveParams) (interface{}, error) {
	var classes []models.Class
	name, isNameProvided := params.Args["name"].(string)
	leaderName, isLeaderNameProvided := params.Args["leaderName"].(string)
	query := DB.Preload("Teacher").Preload("Leader")

	if isNameProvided {
		query = query.Where("LOWER(name) LIKE LOWER(?)", "%"+name+"%")
	}

	if isLeaderNameProvided {
		query = query.Joins("LEFT JOIN users ON classes.leader_id = users.id").
			Where("LOWER(users.fullname) LIKE LOWER(?)", "%"+leaderName+"%")
	}

	if err := query.Find(&classes).Error; err != nil {
		return nil, err
	}

	return classes, nil
}

func GetClassDetail(params graphql.ResolveParams) (interface{}, error) {
	classIDInput, okClass := params.Args["classID"].(int)
	if !okClass {
		return nil, errors.New("invalid classID provided")
	}
	userIDInput, okUser := params.Args["userID"].(int)
	if !okUser {
		return nil, errors.New("userID is required to view class details")
	}

	classID := uint(classIDInput)
	userID := uint(userIDInput)
	var requestingUser models.User
	if err := DB.First(&requestingUser, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("requesting user not found")
		}
		return nil, err
	}

	if !requestingUser.Role {
		return nil, errors.New("authorization error: only teachers can view full class details")
	}

	var class models.Class
	if err := DB.Preload("Teacher").Preload("Leader").Preload("StudentClasses.Student").First(&class, classID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("class not found")
		}
		return nil, err
	}

	return class, nil
}

func GetRegisteredClasses(params graphql.ResolveParams) (interface{}, error) {
	studentID := uint(params.Args["studentID"].(int))
	name, isNameProvided := params.Args["name"].(string)
	teacherName, isTeacherNameProvided := params.Args["teacherName"].(string)

	query := DB.Table("classes").
		Select("classes.*").
		Joins("JOIN student_classes ON classes.id = student_classes.class_id").
		Where("student_classes.student_id = ?", studentID).
		Preload("Teacher")

	if isNameProvided {
		query = query.Where("LOWER(classes.name) LIKE LOWER(?)", "%"+name+"%")
	}

	if isTeacherNameProvided {
		query = query.Joins("JOIN users ON classes.teacher_id = users.id").
			Where("LOWER(users.fullname) LIKE LOWER(?)", "%"+teacherName+"%")
	}

	var classes []models.Class
	if err := query.Find(&classes).Error; err != nil {
		return nil, err
	}

	return classes, nil
}

func GetStudentClassDetail(params graphql.ResolveParams) (interface{}, error) {
	classID := uint(params.Args["classID"].(int))
	studentID := uint(params.Args["studentID"].(int))

	var studentClass models.StudentClass
	if err := DB.Preload("Class.Teacher").Preload("Class.Leader").
		Where("class_id = ? AND student_id = ?", classID, studentID).
		First(&studentClass).Error; err != nil {
		return nil, errors.New("class not found or student not enrolled")
	}

	return studentClass.Class, nil
}

func GetOpenClasses(params graphql.ResolveParams) (interface{}, error) {
	var classes []models.Class

	if err := DB.Preload("Teacher").Preload("Leader").Where("status = ?", true).Find(&classes).Error; err != nil {
		return nil, errors.New("failed to retrieve open classes")
	}

	for i := range classes {
		if classes[i].Teacher != nil {
			classes[i].Teacher.Password = ""
		}
		if classes[i].Leader != nil {
			classes[i].Leader.Password = ""
		}
	}

	return classes, nil
}

func UpdateClass(params graphql.ResolveParams) (interface{}, error) {
	classIDInput, ok := params.Args["classID"].(int)
	if !ok {
		return nil, errors.New("invalid classID provided")
	}
	classID := uint(classIDInput)

	userIDInput, ok := params.Args["userID"].(int)
	if !ok {
		return nil, errors.New("userID is required for authorization")
	}
	userID := uint(userIDInput)

	name, isNameProvided := params.Args["name"].(string)
	subject, isSubjectProvided := params.Args["subject"].(string)
	status, isStatusProvided := params.Args["status"].(bool)
	leaderIDInput, isLeaderIDProvided := params.Args["leaderID"].(int)

	var requestingUser models.User
	if err := DB.First(&requestingUser, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("requesting user not found")
		}
		return nil, err
	}

	if !requestingUser.Role {
		return nil, errors.New("authorization error: only teachers can update classes")
	}

	var class models.Class
	if err := DB.First(&class, classID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("class not found")
		}
		return nil, err
	}

	if class.TeacherID == nil || *class.TeacherID != userID {
		return nil, errors.New("authorization error: you are not the teacher of this class")
	}
	updated := false
	if isNameProvided && class.Name != name {
		class.Name = name
		updated = true
	}

	if isSubjectProvided && class.Subject != subject {
		class.Subject = subject
		updated = true
	}

	if isStatusProvided && class.Status != status {
		class.Status = status
		updated = true
	}

	if isLeaderIDProvided {
		newLeaderID := uint(leaderIDInput)
		var currentLeaderID uint
		if class.LeaderID != nil {
			currentLeaderID = *class.LeaderID
		}

		if newLeaderID != currentLeaderID {
			var potentialLeader models.User
			if err := DB.First(&potentialLeader, newLeaderID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, errors.New("invalid leaderID: user not found")
				}
				return nil, err
			}
			if potentialLeader.Role {
				return nil, errors.New("invalid leaderID: specified user is a teacher")
			}
			class.LeaderID = &newLeaderID
			updated = true
		}
	}

	if !updated {
		DB.Preload("Teacher").Preload("Leader").First(&class, classID)
		return class, nil
	}

	class.UpdatedAt = time.Now()
	if err := DB.Save(&class).Error; err != nil {
		return nil, errors.New("failed to update class")
	}
	if err := DB.Preload("Teacher").Preload("Leader").First(&class, class.ID).Error; err != nil {
		return nil, errors.New("failed to retrieve updated class details")
	}

	return class, nil
}

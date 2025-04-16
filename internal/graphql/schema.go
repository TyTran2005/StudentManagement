package graphql

import (
	"errors"
	"student-management-api/internal/database"
	"student-management-api/internal/models"
	"student-management-api/internal/resolvers"
	"time"

	"github.com/graphql-go/graphql"
)

var DB = database.DB

var UserType = graphql.NewObject(
	graphql.ObjectConfig{
		Name: "User",
		Fields: graphql.Fields{
			"id":       &graphql.Field{Type: graphql.ID},
			"fullname": &graphql.Field{Type: graphql.String},
			"email":    &graphql.Field{Type: graphql.String},
			"role":     &graphql.Field{Type: graphql.Boolean},
		},
	},
)

var ClassType = graphql.NewObject(
	graphql.ObjectConfig{
		Name: "Class",
		Fields: graphql.Fields{
			"id":      &graphql.Field{Type: graphql.ID},
			"name":    &graphql.Field{Type: graphql.String},
			"subject": &graphql.Field{Type: graphql.String},
			"status":  &graphql.Field{Type: graphql.Boolean},
			"teacher": &graphql.Field{
				Type: UserType,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					class, _ := p.Source.(models.Class)
					return class.Teacher, nil
				},
			},
			"leader": &graphql.Field{
				Type: UserType,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					class, _ := p.Source.(models.Class)
					return class.Leader, nil
				},
			},
			"studentCount": &graphql.Field{
				Type: graphql.Int,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					class, ok := p.Source.(models.Class)
					if !ok {
						if classPtr, okPtr := p.Source.(*models.Class); okPtr && classPtr != nil {
							class = *classPtr
						} else {
							return nil, errors.New("internal error: could not determine class source")
						}
					}
					if class.StudentClasses != nil {
						return len(class.StudentClasses), nil
					}
					var count int64

					if resolvers.DB == nil {
						return nil, errors.New("database connection is not available")
					}

					if err := resolvers.DB.Model(&models.StudentClass{}).Where("class_id = ?", class.ID).Count(&count).Error; err != nil {
						return nil, errors.New("failed to count students")
					}
					return int(count), nil
				},
				Description: "Total number of students enrolled in the class.",
			},
			"studentClasses": &graphql.Field{
				Type: graphql.NewList(StudentClassType),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					class, _ := p.Source.(models.Class)
					if class.StudentClasses == nil {
						return []models.StudentClass{}, nil
					}
					return class.StudentClasses, nil
				},
			},
		},
	},
)

var StudentClassType = graphql.NewObject(
	graphql.ObjectConfig{
		Name: "StudentClass",
		Fields: graphql.Fields{
			"student": &graphql.Field{
				Type: UserType,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					studentClass, _ := p.Source.(models.StudentClass)
					return studentClass.Student, nil
				},
			},
			"enrolledAt": &graphql.Field{
				Type: graphql.String,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					studentClass, _ := p.Source.(models.StudentClass)
					return studentClass.EnrolledAt.Format(time.RFC3339), nil
				},
			},
		},
	},
)

var RootQuery = graphql.NewObject(
	graphql.ObjectConfig{
		Name: "RootQuery",
		Fields: graphql.Fields{
			"users": &graphql.Field{
				Type: graphql.NewList(UserType),
				Resolve: func(params graphql.ResolveParams) (interface{}, error) {
					return resolvers.GetUsers()
				},
			},
			"classes": &graphql.Field{
				Type: graphql.NewList(ClassType),
				Args: graphql.FieldConfigArgument{
					"name": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
					"status": &graphql.ArgumentConfig{
						Type:        graphql.Boolean,
						Description: "Lọc lớp theo trạng thái (true: đang mở, false: đã đóng)",
					},
					"leaderID": &graphql.ArgumentConfig{
						Type:        graphql.Int,
						Description: "ID của người lãnh đạo lớp (nếu có)",
					},
					"leaderName": &graphql.ArgumentConfig{
						Type:        graphql.String,
						Description: "Lọc theo tên lớp trưởng",
					},
				},
				Resolve: resolvers.GetClasses,
			},
			"openClasses": &graphql.Field{
				Type:        graphql.NewList(ClassType),
				Resolve:     resolvers.GetOpenClasses,
				Description: "Get a list of classes currently open for enrollment (status=true).",
			},
			"registeredClasses": &graphql.Field{
				Type: graphql.NewList(ClassType),
				Args: graphql.FieldConfigArgument{
					"studentID": &graphql.ArgumentConfig{
						Type: graphql.Int,
					},
					"name": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
					"teacherName": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
				},
				Resolve: resolvers.GetRegisteredClasses,
			},
			"studentClassDetail": &graphql.Field{
				Type: ClassType,
				Args: graphql.FieldConfigArgument{
					"classID": &graphql.ArgumentConfig{
						Type: graphql.Int,
					},
					"studentID": &graphql.ArgumentConfig{
						Type: graphql.Int,
					},
				},
				Resolve: resolvers.GetStudentClassDetail,
			},
			"classDetail": &graphql.Field{
				Type: ClassType,
				Args: graphql.FieldConfigArgument{
					"classID": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.Int),
					},
					"userID": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(graphql.Int),
						Description: "ID of the user requesting details (must be a teacher for full details)",
					},
				},
				Resolve: resolvers.GetClassDetail,
			},
		},
	},
)

var LoginResponseType = graphql.NewObject(
	graphql.ObjectConfig{
		Name: "LoginResponse",
		Fields: graphql.Fields{
			"token": &graphql.Field{Type: graphql.String},
			"user":  &graphql.Field{Type: UserType},
		},
	},
)

var RootMutation = graphql.NewObject(
	graphql.ObjectConfig{
		Name: "RootMutation",
		Fields: graphql.Fields{
			"classes": &graphql.Field{
				Type: graphql.NewList(ClassType),
				Args: graphql.FieldConfigArgument{
					"name": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
					"leaderName": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
				},
				Resolve: resolvers.GetClasses,
			},
			"register": &graphql.Field{
				Type: UserType,
				Args: graphql.FieldConfigArgument{
					"fullname": &graphql.ArgumentConfig{Type: graphql.String},
					"email":    &graphql.ArgumentConfig{Type: graphql.String},
					"password": &graphql.ArgumentConfig{Type: graphql.String},
					"role":     &graphql.ArgumentConfig{Type: graphql.Boolean},
				},
				Resolve: resolvers.RegisterUser,
			},
			"createClass": &graphql.Field{
				Type: ClassType,
				Args: graphql.FieldConfigArgument{
					"name":      &graphql.ArgumentConfig{Type: graphql.String},
					"subject":   &graphql.ArgumentConfig{Type: graphql.String},
					"teacherID": &graphql.ArgumentConfig{Type: graphql.Int},
					"status":    &graphql.ArgumentConfig{Type: graphql.Boolean},
					"leaderID":  &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: resolvers.CreateClass,
			},
			"login": &graphql.Field{
				Type: LoginResponseType,
				Args: graphql.FieldConfigArgument{
					"email":    &graphql.ArgumentConfig{Type: graphql.String},
					"password": &graphql.ArgumentConfig{Type: graphql.String},
				},
				Resolve: resolvers.LoginUser,
			},
			"updateUser": &graphql.Field{
				Type: UserType,
				Args: graphql.FieldConfigArgument{
					"id":       &graphql.ArgumentConfig{Type: graphql.Int},
					"fullname": &graphql.ArgumentConfig{Type: graphql.String},
					"password": &graphql.ArgumentConfig{Type: graphql.String},
				},
				Resolve: resolvers.UpdateUser,
			},
			"joinClass": &graphql.Field{
				Type: StudentClassType,
				Args: graphql.FieldConfigArgument{
					"classID":   &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.Int)},
					"studentID": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.Int)},
				},
				Resolve:     resolvers.JoinClass,
				Description: "Join an open class. Requires student authentication. Returns the enrollment record.",
			},
			"deleteClass": &graphql.Field{
				Type: graphql.String,
				Args: graphql.FieldConfigArgument{
					"classID": &graphql.ArgumentConfig{Type: graphql.Int},
					"userID":  &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: resolvers.DeleteClass,
			},
			"leaveClass": &graphql.Field{
				Type: graphql.String,
				Args: graphql.FieldConfigArgument{
					"classID":   &graphql.ArgumentConfig{Type: graphql.Int},
					"studentID": &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: resolvers.LeaveClass,
			},
			"registeredClasses": &graphql.Field{
				Type: graphql.NewList(ClassType),
				Args: graphql.FieldConfigArgument{
					"studentID": &graphql.ArgumentConfig{
						Type: graphql.Int,
					},
					"name": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
					"teacherName": &graphql.ArgumentConfig{
						Type: graphql.String,
					},
				},
				Resolve: resolvers.GetRegisteredClasses,
			},
			"studentClassDetail": &graphql.Field{
				Type: ClassType,
				Args: graphql.FieldConfigArgument{
					"classID": &graphql.ArgumentConfig{
						Type: graphql.Int,
					},
					"studentID": &graphql.ArgumentConfig{
						Type: graphql.Int,
					},
				},
				Resolve: resolvers.GetStudentClassDetail,
			},
			"classDetail": &graphql.Field{
				Type: ClassType,
				Args: graphql.FieldConfigArgument{
					"classID": &graphql.ArgumentConfig{
						Type: graphql.Int,
					},
					"userID": &graphql.ArgumentConfig{
						Type: graphql.Int,
					},
				},
				Resolve: resolvers.GetClassDetail,
			},
			"updateClass": &graphql.Field{
				Type: ClassType,
				Args: graphql.FieldConfigArgument{
					"classID": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.Int),
					},
					"userID": &graphql.ArgumentConfig{
						Type:        graphql.NewNonNull(graphql.Int),
						Description: "ID of the teacher making the update.",
					},
					"name": &graphql.ArgumentConfig{
						Type:        graphql.String,
						Description: "New name for the class.",
					},
					"subject": &graphql.ArgumentConfig{
						Type:        graphql.String,
						Description: "New subject for the class.",
					},
					"status": &graphql.ArgumentConfig{
						Type:        graphql.Boolean,
						Description: "New status for the class (true: open, false: closed).",
					},
					"leaderID": &graphql.ArgumentConfig{
						Type:        graphql.Int,
						Description: "New leader ID for the class. User must exist and not be a teacher.",
					},
				},
				Resolve:     resolvers.UpdateClass,
				Description: "Allows a teacher to update the basic details of a class they own.",
			},
		},
	},
)

var Schema, _ = graphql.NewSchema(
	graphql.SchemaConfig{
		Query:    RootQuery,
		Mutation: RootMutation,
	},
)

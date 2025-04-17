package graphql

import (
	"errors"
	"log"
	"student-management-api/internal/models"
	"student-management-api/internal/resolvers"
	"time"

	"github.com/graphql-go/graphql"
)

var UserType = graphql.NewObject(
	graphql.ObjectConfig{
		Name: "User",
		Fields: graphql.Fields{
			"id":       &graphql.Field{Type: graphql.ID},
			"fullname": &graphql.Field{Type: graphql.String},
			"email":    &graphql.Field{Type: graphql.String},
			"role":     &graphql.Field{Type: graphql.Boolean, Description: "false: student, true: teacher"},
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
			"status":  &graphql.Field{Type: graphql.Boolean, Description: "true: open, false: closed"},
			"teacher": &graphql.Field{
				Type: UserType,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					switch class := p.Source.(type) {
					case models.Class:
						return class.Teacher, nil
					case *models.Class:
						if class != nil {
							return class.Teacher, nil
						}
					}
					return nil, nil
				},
			},
			"leader": &graphql.Field{
				Type: UserType,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					switch class := p.Source.(type) {
					case models.Class:
						return class.Leader, nil
					case *models.Class:
						if class != nil {
							return class.Leader, nil
						}
					}
					return nil, nil
				},
			},
			"studentCount": &graphql.Field{
				Type: graphql.Int,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					var classID uint
					switch class := p.Source.(type) {
					case models.Class:
						classID = class.ID
						if class.StudentClasses != nil {
							return len(class.StudentClasses), nil
						}
					case *models.Class:
						if class != nil {
							classID = class.ID
							if class.StudentClasses != nil {
								return len(class.StudentClasses), nil
							}
						} else {
							return 0, nil
						}
					default:
						log.Printf("ERROR: Unexpected source type in studentCount resolver: %T", p.Source)
						return nil, errors.New("internal error: could not determine class source")
					}
					if resolvers.DB == nil {
						log.Println("ERROR: Database connection is not available in studentCount resolver")
						return nil, errors.New("database connection is not available")
					}

					var count int64
					if err := resolvers.DB.Model(&models.StudentClass{}).
						Where("class_id = ? AND left_at IS NULL", classID).
						Count(&count).Error; err != nil {
						log.Printf("ERROR: Failed to count students for class %d: %v", classID, err)
						return nil, errors.New("failed to count students")
					}
					return int(count), nil
				},
				Description: "Total number of currently enrolled students in the class.",
			},
			"studentClasses": &graphql.Field{
				Type: graphql.NewList(StudentClassType),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {

					switch class := p.Source.(type) {
					case models.Class:
						if class.StudentClasses != nil {
							return class.StudentClasses, nil
						}
					case *models.Class:
						if class != nil && class.StudentClasses != nil {
							return class.StudentClasses, nil
						}
					}
					return []models.StudentClass{}, nil
				},
				Description: "List of student enrollment records (visible to teachers).",
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
					switch sc := p.Source.(type) {
					case models.StudentClass:
						return sc.Student, nil
					case *models.StudentClass:
						if sc != nil {
							return sc.Student, nil
						}
					}
					return nil, nil
				},
			},
			"enrolledAt": &graphql.Field{
				Type: graphql.String,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					var enrolledAt time.Time
					switch sc := p.Source.(type) {
					case models.StudentClass:
						enrolledAt = sc.EnrolledAt
					case *models.StudentClass:
						if sc != nil {
							enrolledAt = sc.EnrolledAt
						} else {
							return nil, nil
						}
					default:
						return nil, errors.New("internal error: unexpected source type for enrolledAt")
					}
					if !enrolledAt.IsZero() {
						return enrolledAt.Format(time.RFC3339), nil
					}
					return nil, nil
				},
			},
			"classID": &graphql.Field{
				Type: graphql.Int,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					switch sc := p.Source.(type) {
					case models.StudentClass:
						return int(sc.ClassID), nil
					case *models.StudentClass:
						if sc != nil {
							return int(sc.ClassID), nil
						}
					}
					return nil, nil
				},
			},
			"studentID": &graphql.Field{
				Type: graphql.Int,
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					switch sc := p.Source.(type) {
					case models.StudentClass:
						return int(sc.StudentID), nil
					case *models.StudentClass:
						if sc != nil {
							return int(sc.StudentID), nil
						}
					}
					return nil, nil
				},
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

var RootQuery = graphql.NewObject(
	graphql.ObjectConfig{
		Name: "RootQuery",
		Fields: graphql.Fields{
			"users": &graphql.Field{
				Type:        graphql.NewList(UserType),
				Resolve:     resolvers.GetUsers,
				Description: "Get a list of all users (requires appropriate permissions).",
			},
			"classes": &graphql.Field{
				Type: graphql.NewList(ClassType),
				Args: graphql.FieldConfigArgument{
					"name": &graphql.ArgumentConfig{
						Type:        graphql.String,
						Description: "Filter classes by name (case-insensitive, partial match).",
					},
					"status": &graphql.ArgumentConfig{
						Type:        graphql.Boolean,
						Description: "Filter classes by status (true: open, false: closed).",
					},

					"leaderName": &graphql.ArgumentConfig{
						Type:        graphql.String,
						Description: "Filter classes by leader's name (case-insensitive, partial match).",
					},
				},
				Resolve:     resolvers.GetClasses,
				Description: "Get a list of classes with filters (teacher access recommended).",
			},

			"openClasses": &graphql.Field{
				Type:        graphql.NewList(ClassType),
				Resolve:     resolvers.GetOpenClasses,
				Description: "Get a list of classes currently open for enrollment (status=true).",
			},
			"registeredClasses": &graphql.Field{
				Type: graphql.NewList(ClassType),
				Args: graphql.FieldConfigArgument{
					"name": &graphql.ArgumentConfig{
						Type:        graphql.String,
						Description: "Filter registered classes by name (case-insensitive, partial match).",
					},
					"teacherName": &graphql.ArgumentConfig{
						Type:        graphql.String,
						Description: "Filter registered classes by teacher's name (case-insensitive, partial match).",
					},
				},
				Resolve:     resolvers.GetRegisteredClasses,
				Description: "Get the list of classes the logged-in student is registered for.",
			},
			"studentClassDetail": &graphql.Field{
				Type: ClassType,
				Args: graphql.FieldConfigArgument{
					"classID": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.Int),
					},
				},
				Resolve:     resolvers.GetStudentClassDetail,
				Description: "Get details of a specific class the logged-in student is registered for.",
			},

			"classDetail": &graphql.Field{
				Type: ClassType,
				Args: graphql.FieldConfigArgument{
					"classID": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.Int),
					},
				},
				Resolve:     resolvers.GetClassDetail,
				Description: "Get detailed information about a class, including student list (teacher access required).",
			},

			"me": &graphql.Field{
				Type:        UserType,
				Resolve:     resolvers.GetCurrentUser,
				Description: "Get information about the currently logged-in user.",
			},
		},
	},
)

var RootMutation = graphql.NewObject(
	graphql.ObjectConfig{
		Name: "RootMutation",
		Fields: graphql.Fields{

			"register": &graphql.Field{
				Type: UserType,
				Args: graphql.FieldConfigArgument{
					"fullname": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"email":    &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"password": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"role":     &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.Boolean), Description: "false: student, true: teacher"},
				},
				Resolve: resolvers.RegisterUser,
			},

			"login": &graphql.Field{
				Type: LoginResponseType,
				Args: graphql.FieldConfigArgument{
					"email":    &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"password": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
				},
				Resolve: resolvers.LoginUser,
			},

			"updateUser": &graphql.Field{
				Type: UserType,
				Args: graphql.FieldConfigArgument{
					"fullname": &graphql.ArgumentConfig{Type: graphql.String},
					"password": &graphql.ArgumentConfig{Type: graphql.String},
				},
				Resolve:     resolvers.UpdateUser,
				Description: "Update the logged-in user's fullname or password.",
			},

			"createClass": &graphql.Field{
				Type: ClassType,
				Args: graphql.FieldConfigArgument{
					"name":    &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"subject": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"status":  &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.Boolean), Description: "Initial status (true: open, false: closed)"},
					"leaderID": &graphql.ArgumentConfig{
						Type:        graphql.Int,
						Description: "Optional ID of the student user to be the class leader.",
					},
				},
				Resolve:     resolvers.CreateClass,
				Description: "Create a new class (teacher access required).",
			},
			"updateClass": &graphql.Field{
				Type: ClassType,
				Args: graphql.FieldConfigArgument{
					"classID": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.Int),
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
						Description: "New leader ID for the class. User must exist and be a student.",
					},
				},
				Resolve:     resolvers.UpdateClass,
				Description: "Update details of a class owned by the logged-in teacher.",
			},

			"deleteClass": &graphql.Field{
				Type: graphql.String,
				Args: graphql.FieldConfigArgument{
					"classID": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.Int),
					},
				},
				Resolve:     resolvers.DeleteClass,
				Description: "Delete a class owned by the logged-in teacher (fails if >= 5 students).",
			},
			"joinClass": &graphql.Field{
				Type: StudentClassType,
				Args: graphql.FieldConfigArgument{
					"classID": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.Int),
					},
				},
				Resolve:     resolvers.JoinClass,
				Description: "Allows the logged-in student to join an open class.",
			},
			"leaveClass": &graphql.Field{
				Type: graphql.String,
				Args: graphql.FieldConfigArgument{
					"classID": &graphql.ArgumentConfig{
						Type: graphql.NewNonNull(graphql.Int),
					},
				},
				Resolve:     resolvers.LeaveClass,
				Description: "Allows the logged-in student to leave a class they are enrolled in.",
			},
		},
	},
)

var Schema graphql.Schema

func init() {
	var err error
	Schema, err = graphql.NewSchema(
		graphql.SchemaConfig{
			Query:    RootQuery,
			Mutation: RootMutation,
		},
	)
	if err != nil {
		log.Fatalf("FATAL: Failed to create GraphQL schema: %v", err)
	}
	log.Println("INFO: GraphQL schema created successfully.")
}

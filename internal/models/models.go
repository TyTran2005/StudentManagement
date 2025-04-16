package models

import (
	"time"
)

type User struct {
	ID        uint      `gorm:"primaryKey;autoIncrement"`
	Fullname  string    `gorm:"type:varchar(50);not null"`
	Email     string    `gorm:"type:varchar(50);not null;unique"`
	Password  string    `gorm:"type:varchar(255);not null"`
	Role      bool      `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`

	ClassesTaught  []Class        `gorm:"foreignKey:TeacherID"`
	ClassesLed     []Class        `gorm:"foreignKey:LeaderID"`
	StudentClasses []StudentClass `gorm:"foreignKey:StudentID"`
}

type Class struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	Name      string `gorm:"type:varchar(50);not null"`
	Subject   string `gorm:"type:varchar(50);not null"`
	Status    bool   `gorm:"not null"`
	TeacherID *uint
	LeaderID  *uint
	CreatedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP"`

	Teacher        *User          `gorm:"foreignKey:TeacherID"`
	Leader         *User          `gorm:"foreignKey:LeaderID"`
	StudentClasses []StudentClass `gorm:"foreignKey:ClassID"`
}

type StudentClass struct {
	StudentID  uint       `gorm:"primaryKey"`
	ClassID    uint       `gorm:"primaryKey"`
	EnrolledAt time.Time  `gorm:"not null;default:CURRENT_TIMESTAMP"`
	LeftAt     *time.Time `gorm:"null"`

	Student User  `gorm:"foreignKey:StudentID"`
	Class   Class `gorm:"foreignKey:ClassID"`
}

func (User) TableName() string {
	return "users"
}

func (Class) TableName() string {
	return "classes"
}

func (StudentClass) TableName() string {
	return "student_classes"
}

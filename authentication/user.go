package authentication

type User interface {
	FullName() string
	GetID() string
	GetUsername() string
	GetEmail() string
	GetFirstName() string
	GetLastName() string
}

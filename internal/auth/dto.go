package auth

type RegisterDto struct {
	Email     string  `json:"email"     binding:"required,email"`
	Password  string  `json:"password"  binding:"required,min=6"`
	FirstName *string `json:"firstName" binding:"omitempty,max=50"`
	LastName  *string `json:"lastName"  binding:"omitempty,max=50"`
}

type LoginDto struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

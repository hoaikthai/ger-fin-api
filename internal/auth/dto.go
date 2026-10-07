package auth

type RegisterDto struct {
	Email     string  `json:"email"     binding:"required,email"`
	Password  string  `json:"password"  binding:"required,min=8,max=72"`
	FirstName *string `json:"firstName" binding:"omitempty,max=50"`
	LastName  *string `json:"lastName"  binding:"omitempty,max=50"`
}

type LoginDto struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse is the response body for a successful login.
// registerAcceptedMessage is the response to every valid registration request,
// whether or not the email was already registered (prevents account enumeration).
const registerAcceptedMessage = "If this email is not already registered, the account has been created. Please log in."

type LoginResponse struct {
	AccessToken string `json:"access_token"`
}

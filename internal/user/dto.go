package user

type UpdateProfileDto struct {
	FirstName *string `json:"firstName" binding:"omitempty,max=50"`
	LastName  *string `json:"lastName"  binding:"omitempty,max=50"`
}

package domain

type User struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	Timezone    string    `json:"timezone"`
	HasPassword bool      `json:"has_password"`
	Progress    *Progress `json:"progress,omitempty"`
}

package domain

type User struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Timezone string    `json:"timezone"`
	Progress *Progress `json:"progress,omitempty"`
}

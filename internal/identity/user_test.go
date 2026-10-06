package identity

import "testing"

func TestUser_ToPublic(t *testing.T) {
	t.Parallel()
	u := &User{
		ID:       "user_01",
		Username: "Katakuri",
		Email:    "k@example.com",
		Password: "$2a$12$verifier",
		Role:     RoleAdmin,
		Status:   StatusActive,
	}

	got := u.ToPublic()
	want := PublicUser{ID: "user_01", Username: "Katakuri", Role: RoleAdmin}
	if got != want {
		t.Errorf("ToPublic() = %+v, want %+v", got, want)
	}
}

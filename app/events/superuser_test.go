package events

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSuperUser_IsSuper(t *testing.T) {
	tests := []struct {
		name     string
		super    SuperUser
		userName string
		want     bool
	}{
		{
			name:     "User is a super user",
			super:    SuperUser{"Alice", "Bob"},
			userName: "Alice",
			want:     true,
		},
		{
			name:     "User is not a super user",
			super:    SuperUser{"Alice", "Bob"},
			userName: "Charlie",
			want:     false,
		},
		{
			name:     "User is a super user with slash prefix",
			super:    SuperUser{"/Alice", "Bob"},
			userName: "Alice",
			want:     true,
		},
		{
			name:     "User is not a super user with slash prefix",
			super:    SuperUser{"/Alice", "Bob"},
			userName: "Charlie",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.super.IsSuper(tt.userName))
		})
	}
}

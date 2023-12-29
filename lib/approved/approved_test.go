package approved

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUserInfo_String(t *testing.T) {
	tests := []struct {
		name     string
		userInfo UserInfo
		expected string
	}{
		{
			name: "with username and userid",
			userInfo: UserInfo{
				UserID:    "123",
				UserName:  "John Doe",
				Timestamp: time.Now(),
				Count:     5,
			},
			expected: `"John Doe" (123)`,
		},
		{
			name: "without username",
			userInfo: UserInfo{
				UserID:    "123",
				Timestamp: time.Now(),
				Count:     5,
			},
			expected: `"123"`,
		},
	}

	// Run test cases
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.userInfo.String()
			assert.Equal(t, tt.expected, actual)
		})
	}
}

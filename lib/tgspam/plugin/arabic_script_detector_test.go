package plugin

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestArabicScriptDetector(t *testing.T) {
	// create a new checker
	checker := NewChecker()
	defer checker.Close()

	// load the test script
	scriptPath := filepath.Join("..", "testdata", "arabic_script_detector_test.lua")
	err := checker.LoadScript(scriptPath)
	require.NoError(t, err)

	// get the check function
	check, err := checker.GetCheck("arabic_script_detector_test")
	require.NoError(t, err)

	tests := []struct {
		name     string
		userName string
		wantSpam bool
		details  string
	}{
		{
			name:     "latin username",
			userName: "JohnDoe",
			wantSpam: false,
			details:  "username does not contain Arabic script",
		},
		{
			name:     "arabic username",
			userName: "مصطفي الصعيدي",
			wantSpam: true,
			details:  "username contains Arabic script which may be unusual for this chat",
		},
		{
			name:     "arabic with latin",
			userName: "Ahmed الصعيدي",
			wantSpam: true,
			details:  "username contains Arabic script which may be unusual for this chat",
		},
		{
			name:     "latin with numbers and spaces",
			userName: "John Doe 123",
			wantSpam: false,
			details:  "username does not contain Arabic script",
		},
		{
			name:     "empty username",
			userName: "",
			wantSpam: false,
			details:  "no username to check",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := check(spamcheck.Request{
				UserName: tt.userName,
			})

			assert.Equal(t, tt.wantSpam, resp.Spam)
			assert.Equal(t, tt.details, resp.Details)
			assert.Equal(t, "lua-arabic_script_detector_test", resp.Name)
		})
	}
}

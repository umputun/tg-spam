package events

import (
	"strings"
)

// SuperUser for moderators
type SuperUser []string

// IsSuper checks if username in su list
func (s SuperUser) IsSuper(userName string) bool {
	for _, super := range s {
		if strings.EqualFold(userName, super) || strings.EqualFold("/"+userName, super) {
			return true
		}
	}
	return false
}

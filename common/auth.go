package common

import (
	"regexp"
	"unicode/utf8"
)

const (
	MinUserLength     = 1
	MaxUserLength     = 16
	MinPasswordLength = 6
	MaxPasswordLength = 72

	LoginPath    = "/login"
	RegisterPath = "/register"
	SessionPath  = "/session"
)

type AuthRequest struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string `json:"token"`
}

type SessionInfoResponse struct {
	User string `json:"user"`
}

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

func ValidAuthRequest(req AuthRequest) bool {
	return len(req.User) >= MinUserLength &&
		len(req.User) <= MaxUserLength &&
		usernamePattern.MatchString(req.User) &&
		len(req.Password) >= MinPasswordLength &&
		len(req.Password) <= MaxPasswordLength &&
		utf8.ValidString(req.Password)
}

const (
	DirectoriesPath = "/directories"
)

type DirectoryEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type CreateDirectoryRequest struct {
	Path string `json:"path"`
}

package models

type User struct {
	ID       int64  `json:"id"`
	Nickname string `json:"nickname"`
	Password string `json:"-"`
}

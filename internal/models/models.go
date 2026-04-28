// Package models defines the data structures shared across the application.
package models

// User represents a registered forum user.
type User struct {
	ID           int
	Email        string
	Username     string
	PasswordHash string
}

// Category represents a post category (subforum topic).
type Category struct {
	ID   int
	Name string
}

// Post represents a forum post with aggregated reaction counts and its categories.
type Post struct {
	ID          int
	UserID      int
	Username    string
	Title       string
	Content     string
	ImagePath   string
	CreatedAt   string
	Categories  []Category
	Likes       int
	Dislikes    int
	UserReacted int // 1 = liked, -1 = disliked, 0 = no reaction
}

// Comment represents a comment on a post with aggregated reaction counts.
type Comment struct {
	ID          int
	PostID      int
	UserID      int
	Username    string
	Content     string
	CreatedAt   string
	Likes       int
	Dislikes    int
	UserReacted int // 1 = liked, -1 = disliked, 0 = no reaction
}

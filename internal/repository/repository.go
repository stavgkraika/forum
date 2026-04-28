// Package repository provides database access methods for all forum entities.
package repository

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"forum/internal/models"
)

// Repository wraps a sql.DB and exposes all data access methods.
type Repository struct{ DB *sql.DB }

// New returns a new Repository backed by the given database connection.
func New(db *sql.DB) *Repository { return &Repository{DB: db} }

// CreateUser inserts a new user with a bcrypt-hashed password.
func (r *Repository) CreateUser(email, username, hash string) error {
	_, err := r.DB.Exec(`INSERT INTO users(email, username, password_hash) VALUES(?,?,?)`, email, username, hash)
	return err
}

// FindOrCreateOAuthUser returns an existing OAuth-backed user or creates one.
func (r *Repository) FindOrCreateOAuthUser(email, username, hash string) (models.User, error) {
	if u, err := r.UserByEmail(email); err == nil {
		return u, nil
	}
	for i := 0; i < 10; i++ {
		candidate := username
		if i > 0 {
			suffix := "-" + strconv.Itoa(i)
			if len(candidate)+len(suffix) > 30 {
				candidate = candidate[:30-len(suffix)]
			}
			candidate += suffix
		}
		if err := r.CreateUser(email, candidate, hash); err == nil {
			return r.UserByEmail(email)
		}
	}
	return models.User{}, errors.New("could not create oauth user")
}

// UserByEmail returns the user with the given email address.
func (r *Repository) UserByEmail(email string) (models.User, error) {
	var u models.User
	err := r.DB.QueryRow(`SELECT id,email,username,password_hash FROM users WHERE email=?`, email).
		Scan(&u.ID, &u.Email, &u.Username, &u.PasswordHash)
	return u, err
}

// UserBySession returns the user associated with a valid, non-expired session ID.
func (r *Repository) UserBySession(sessionID string) (models.User, error) {
	var u models.User
	err := r.DB.QueryRow(`SELECT u.id,u.email,u.username,u.password_hash
		FROM users u JOIN sessions s ON s.user_id=u.id WHERE s.id=? AND s.expires_at > CURRENT_TIMESTAMP`, sessionID).
		Scan(&u.ID, &u.Email, &u.Username, &u.PasswordHash)
	return u, err
}

// CreateSession replaces any existing session for the user and inserts a new one.
func (r *Repository) CreateSession(id string, userID int, expires time.Time) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO sessions(id,user_id,expires_at) VALUES(?,?,?)`, id, userID, expires.UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteSession removes the session with the given ID (logout).
func (r *Repository) DeleteSession(id string) error {
	_, err := r.DB.Exec(`DELETE FROM sessions WHERE id=?`, id)
	return err
}

// Categories returns all categories ordered alphabetically.
func (r *Repository) Categories() ([]models.Category, error) {
	rows, err := r.DB.Query(`SELECT id,name FROM categories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Category
	for rows.Next() {
		var c models.Category
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreatePost inserts a post and associates it with the given category IDs in a transaction.
func (r *Repository) CreatePost(userID int, title, content string, categoryIDs []int) error {
	return r.CreatePostWithImage(userID, title, content, "", categoryIDs)
}

// CreatePostWithImage inserts a post, optional image URL, and category associations.
func (r *Repository) CreatePostWithImage(userID int, title, content, imagePath string, categoryIDs []int) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO posts(user_id,title,content,image_path) VALUES(?,?,?,?)`, userID, title, content, nullString(imagePath))
	if err != nil {
		return err
	}
	postID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	for _, id := range categoryIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO post_categories(post_id,category_id) VALUES(?,?)`, postID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Posts returns posts filtered by category, "created" (by userID) or "liked" (by userID).
func (r *Repository) Posts(filter string, categoryID, userID int) ([]models.Post, error) {
	where := []string{}
	args := []any{}
	joins := ""

	if categoryID > 0 {
		joins += ` JOIN post_categories pc ON pc.post_id=p.id`
		where = append(where, "pc.category_id=?")
		args = append(args, categoryID)
	}
	if filter == "created" {
		where = append(where, "p.user_id=?")
		args = append(args, userID)
	}
	if filter == "liked" {
		// Use a subquery to avoid conflicting with the pr alias used for aggregation.
		where = append(where, "EXISTS (SELECT 1 FROM post_reactions WHERE post_id=p.id AND user_id=? AND value=1)")
		args = append(args, userID)
	}

	q := `SELECT p.id,p.user_id,u.username,p.title,p.content,COALESCE(p.image_path,''),CAST(p.created_at AS TEXT),
		COALESCE(SUM(CASE WHEN pr.value=1 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN pr.value=-1 THEN 1 ELSE 0 END),0)
		FROM posts p JOIN users u ON u.id=p.user_id LEFT JOIN post_reactions pr ON pr.post_id=p.id` + joins
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += ` GROUP BY p.id ORDER BY p.created_at DESC`

	rows, err := r.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []models.Post
	for rows.Next() {
		var p models.Post
		if err := rows.Scan(&p.ID, &p.UserID, &p.Username, &p.Title, &p.Content, &p.ImagePath, &p.CreatedAt, &p.Likes, &p.Dislikes); err != nil {
			return nil, err
		}
		p.Categories, _ = r.PostCategories(p.ID)
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// Post returns a single post by ID with reaction counts and the viewer's own reaction.
func (r *Repository) Post(id, viewerID int) (models.Post, error) {
	var p models.Post
	err := r.DB.QueryRow(`SELECT p.id,p.user_id,u.username,p.title,p.content,COALESCE(p.image_path,''),CAST(p.created_at AS TEXT),
	COALESCE(SUM(CASE WHEN pr.value=1 THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN pr.value=-1 THEN 1 ELSE 0 END),0),
	COALESCE((SELECT value FROM post_reactions WHERE post_id=p.id AND user_id=?),0)
	FROM posts p JOIN users u ON u.id=p.user_id LEFT JOIN post_reactions pr ON pr.post_id=p.id WHERE p.id=? GROUP BY p.id`, viewerID, id).
		Scan(&p.ID, &p.UserID, &p.Username, &p.Title, &p.Content, &p.ImagePath, &p.CreatedAt, &p.Likes, &p.Dislikes, &p.UserReacted)
	if err != nil {
		return p, err
	}
	p.Categories, _ = r.PostCategories(p.ID)
	return p, nil
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

// PostCategories returns the categories associated with a post.
func (r *Repository) PostCategories(postID int) ([]models.Category, error) {
	rows, err := r.DB.Query(`SELECT c.id,c.name FROM categories c JOIN post_categories pc ON pc.category_id=c.id WHERE pc.post_id=? ORDER BY c.name`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cats []models.Category
	for rows.Next() {
		var c models.Category
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

// Comments returns all comments for a post with reaction counts and the viewer's own reaction.
func (r *Repository) Comments(postID, viewerID int) ([]models.Comment, error) {
	rows, err := r.DB.Query(`SELECT c.id,c.post_id,c.user_id,u.username,c.content,CAST(c.created_at AS TEXT),
	COALESCE(SUM(CASE WHEN cr.value=1 THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN cr.value=-1 THEN 1 ELSE 0 END),0),
	COALESCE((SELECT value FROM comment_reactions WHERE comment_id=c.id AND user_id=?),0)
	FROM comments c JOIN users u ON u.id=c.user_id LEFT JOIN comment_reactions cr ON cr.comment_id=c.id WHERE c.post_id=? GROUP BY c.id ORDER BY c.created_at`, viewerID, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Comment
	for rows.Next() {
		var c models.Comment
		if err := rows.Scan(&c.ID, &c.PostID, &c.UserID, &c.Username, &c.Content, &c.CreatedAt, &c.Likes, &c.Dislikes, &c.UserReacted); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateComment inserts a new comment on a post.
func (r *Repository) CreateComment(postID, userID int, content string) error {
	_, err := r.DB.Exec(`INSERT INTO comments(post_id,user_id,content) VALUES(?,?,?)`, postID, userID, content)
	return err
}

// ReactPost records or updates a like/dislike reaction on a post.
func (r *Repository) ReactPost(userID, postID, value int) error {
	return r.react("post_reactions", "post_id", userID, postID, value)
}

// ReactComment records or updates a like/dislike reaction on a comment.
func (r *Repository) ReactComment(userID, commentID, value int) error {
	return r.react("comment_reactions", "comment_id", userID, commentID, value)
}

// react is the shared implementation for post and comment reactions.
// value must be 1 (like), -1 (dislike), or 0 (remove reaction).
func (r *Repository) react(table, col string, userID, targetID, value int) error {
	if value != -1 && value != 0 && value != 1 {
		return errors.New("invalid reaction")
	}
	if value == 0 {
		_, err := r.DB.Exec(`DELETE FROM `+table+` WHERE user_id=? AND `+col+`=?`, userID, targetID)
		return err
	}
	_, err := r.DB.Exec(`INSERT OR REPLACE INTO `+table+`(user_id,`+col+`,value) VALUES(?,?,?)`, userID, targetID, value)
	return err
}

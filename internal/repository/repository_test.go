package repository

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// openTestDB opens an in-memory SQLite DB and applies the real schema.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	// Find schema relative to the module root.
	schemaPath := filepath.Join("..", "..", "migrations", "schema.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return db
}

func TestCreateAndGetUser(t *testing.T) {
	r := New(openTestDB(t))
	if err := r.CreateUser("a@b.com", "alice", "hash"); err != nil {
		t.Fatal(err)
	}
	u, err := r.UserByEmail("a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "alice" {
		t.Errorf("got username %q, want %q", u.Username, "alice")
	}
	if u.PasswordHash != "hash" {
		t.Errorf("got hash %q, want %q", u.PasswordHash, "hash")
	}
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	if err := r.CreateUser("a@b.com", "other", "hash"); err == nil {
		t.Fatal("expected error for duplicate email")
	}
}

func TestCreateUser_DuplicateUsername(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	if err := r.CreateUser("b@b.com", "alice", "hash"); err == nil {
		t.Fatal("expected error for duplicate username")
	}
}

func TestUserByEmail_NotFound(t *testing.T) {
	r := New(openTestDB(t))
	_, err := r.UserByEmail("nobody@b.com")
	if err == nil {
		t.Fatal("expected error for missing user")
	}
}

func TestSession(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	u, _ := r.UserByEmail("a@b.com")

	expires := time.Now().Add(time.Hour)
	if err := r.CreateSession("sess1", u.ID, expires); err != nil {
		t.Fatal(err)
	}
	got, err := r.UserBySession("sess1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID {
		t.Errorf("got user ID %d, want %d", got.ID, u.ID)
	}
}

func TestSession_Expired(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	u, _ := r.UserByEmail("a@b.com")

	// Create an already-expired session.
	expires := time.Now().Add(-time.Hour)
	r.CreateSession("expired", u.ID, expires)
	_, err := r.UserBySession("expired")
	if err == nil {
		t.Fatal("expected error for expired session")
	}
}

func TestSession_Replace(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	u, _ := r.UserByEmail("a@b.com")
	expires := time.Now().Add(time.Hour)

	r.CreateSession("sess1", u.ID, expires)
	// Creating a second session should replace the first.
	if err := r.CreateSession("sess2", u.ID, expires); err != nil {
		t.Fatal(err)
	}
	if _, err := r.UserBySession("sess1"); err == nil {
		t.Fatal("old session should be deleted")
	}
	if _, err := r.UserBySession("sess2"); err != nil {
		t.Fatal("new session should be valid")
	}
}

func TestDeleteSession(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	u, _ := r.UserByEmail("a@b.com")
	r.CreateSession("sess1", u.ID, time.Now().Add(time.Hour))

	if err := r.DeleteSession("sess1"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.UserBySession("sess1"); err == nil {
		t.Fatal("deleted session should not be valid")
	}
}

func TestCategories(t *testing.T) {
	r := New(openTestDB(t))
	cats, err := r.Categories()
	if err != nil {
		t.Fatal(err)
	}
	// Schema seeds 5 default categories.
	if len(cats) == 0 {
		t.Fatal("expected seeded categories, got none")
	}
}

func TestCreateAndGetPost(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	u, _ := r.UserByEmail("a@b.com")
	cats, _ := r.Categories()

	if err := r.CreatePost(u.ID, "Hello", "World", []int{cats[0].ID}); err != nil {
		t.Fatal(err)
	}
	posts, err := r.Posts("", 0, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	if posts[0].Title != "Hello" {
		t.Errorf("got title %q, want %q", posts[0].Title, "Hello")
	}
}

func TestPosts_FilterCreated(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	r.CreateUser("b@b.com", "bob", "hash")
	alice, _ := r.UserByEmail("a@b.com")
	bob, _ := r.UserByEmail("b@b.com")
	cats, _ := r.Categories()

	r.CreatePost(alice.ID, "Alice post", "content", []int{cats[0].ID})
	r.CreatePost(bob.ID, "Bob post", "content", []int{cats[0].ID})

	posts, _ := r.Posts("created", 0, alice.ID)
	if len(posts) != 1 || posts[0].Username != "alice" {
		t.Errorf("filter=created should return only alice's post, got %d posts", len(posts))
	}
}

func TestPosts_FilterLiked(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	u, _ := r.UserByEmail("a@b.com")
	cats, _ := r.Categories()

	r.CreatePost(u.ID, "Post 1", "content", []int{cats[0].ID})
	r.CreatePost(u.ID, "Post 2", "content", []int{cats[0].ID})
	all, _ := r.Posts("", 0, u.ID)

	// Like only the first post.
	r.ReactPost(u.ID, all[0].ID, 1)

	liked, _ := r.Posts("liked", 0, u.ID)
	if len(liked) != 1 {
		t.Errorf("expected 1 liked post, got %d", len(liked))
	}
}

func TestPosts_FilterCategory(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	u, _ := r.UserByEmail("a@b.com")
	cats, _ := r.Categories()

	r.CreatePost(u.ID, "Post cat0", "content", []int{cats[0].ID})
	r.CreatePost(u.ID, "Post cat1", "content", []int{cats[1].ID})

	posts, _ := r.Posts("", cats[0].ID, u.ID)
	if len(posts) != 1 || posts[0].Title != "Post cat0" {
		t.Errorf("category filter returned wrong posts: %v", posts)
	}
}

func TestGetPost(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	u, _ := r.UserByEmail("a@b.com")
	cats, _ := r.Categories()
	r.CreatePost(u.ID, "Title", "Body", []int{cats[0].ID})
	all, _ := r.Posts("", 0, u.ID)

	p, err := r.Post(all[0].ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Title" {
		t.Errorf("got title %q, want %q", p.Title, "Title")
	}
}

func TestPost_NotFound(t *testing.T) {
	r := New(openTestDB(t))
	_, err := r.Post(9999, 0)
	if err == nil {
		t.Fatal("expected error for missing post")
	}
}

func TestCreateAndGetComment(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	u, _ := r.UserByEmail("a@b.com")
	cats, _ := r.Categories()
	r.CreatePost(u.ID, "Title", "Body", []int{cats[0].ID})
	posts, _ := r.Posts("", 0, u.ID)

	if err := r.CreateComment(posts[0].ID, u.ID, "Nice post!"); err != nil {
		t.Fatal(err)
	}
	comments, err := r.Comments(posts[0].ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].Content != "Nice post!" {
		t.Errorf("unexpected comments: %v", comments)
	}
}

func TestReactPost(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	u, _ := r.UserByEmail("a@b.com")
	cats, _ := r.Categories()
	r.CreatePost(u.ID, "Title", "Body", []int{cats[0].ID})
	posts, _ := r.Posts("", 0, u.ID)
	postID := posts[0].ID

	// Like.
	if err := r.ReactPost(u.ID, postID, 1); err != nil {
		t.Fatal(err)
	}
	p, _ := r.Post(postID, u.ID)
	if p.Likes != 1 || p.UserReacted != 1 {
		t.Errorf("expected 1 like, got likes=%d reacted=%d", p.Likes, p.UserReacted)
	}

	// Switch to dislike.
	r.ReactPost(u.ID, postID, -1)
	p, _ = r.Post(postID, u.ID)
	if p.Dislikes != 1 || p.Likes != 0 {
		t.Errorf("expected 1 dislike after switch, got likes=%d dislikes=%d", p.Likes, p.Dislikes)
	}

	// Clear reaction.
	r.ReactPost(u.ID, postID, 0)
	p, _ = r.Post(postID, u.ID)
	if p.Likes != 0 || p.Dislikes != 0 {
		t.Errorf("expected 0 reactions after clear, got likes=%d dislikes=%d", p.Likes, p.Dislikes)
	}
}

func TestReactPost_Invalid(t *testing.T) {
	r := New(openTestDB(t))
	if err := r.ReactPost(1, 1, 99); err == nil {
		t.Fatal("expected error for invalid reaction value")
	}
}

func TestReactComment(t *testing.T) {
	r := New(openTestDB(t))
	r.CreateUser("a@b.com", "alice", "hash")
	u, _ := r.UserByEmail("a@b.com")
	cats, _ := r.Categories()
	r.CreatePost(u.ID, "Title", "Body", []int{cats[0].ID})
	posts, _ := r.Posts("", 0, u.ID)
	r.CreateComment(posts[0].ID, u.ID, "comment")
	comments, _ := r.Comments(posts[0].ID, u.ID)
	cID := comments[0].ID

	if err := r.ReactComment(u.ID, cID, 1); err != nil {
		t.Fatal(err)
	}
	comments, _ = r.Comments(posts[0].ID, u.ID)
	if comments[0].Likes != 1 {
		t.Errorf("expected 1 like on comment, got %d", comments[0].Likes)
	}

	r.ReactComment(u.ID, cID, 0)
	comments, _ = r.Comments(posts[0].ID, u.ID)
	if comments[0].Likes != 0 {
		t.Errorf("expected 0 likes after clear, got %d", comments[0].Likes)
	}
}

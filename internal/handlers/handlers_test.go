package handlers

import (
	"bytes"
	"database/sql"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"forum/internal/repository"
)

// newTestApp creates an App backed by an in-memory SQLite DB with the real schema.
func newTestApp(t *testing.T) *App {
	t.Helper()
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
	repo := repository.New(db)
	templateGlob := filepath.Join("..", "..", "templates", "*.html")
	app, err := NewWithTemplates(repo, templateGlob)
	if err != nil {
		t.Fatalf("NewWithTemplates() error: %v", err)
	}
	return app
}

func TestHome_GET(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	app.Home(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Home GET = %d, want 200", w.Code)
	}
}

func TestHome_NotFound(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/notapage", nil)
	w := httptest.NewRecorder()
	app.Home(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("Home unknown path = %d, want 404", w.Code)
	}
}

func TestHome_MethodNotAllowed(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	app.Home(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Home POST = %d, want 405", w.Code)
	}
}

func TestRegister_GET(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/register", nil)
	w := httptest.NewRecorder()
	app.Register(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Register GET = %d, want 200", w.Code)
	}
}

func TestRegister_POST_Success(t *testing.T) {
	app := newTestApp(t)
	form := url.Values{"email": {"a@b.com"}, "username": {"alice"}, "password": {"password123"}}
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	app.Register(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("Register POST success = %d, want 303", w.Code)
	}
}

func TestRegister_POST_InvalidInput(t *testing.T) {
	app := newTestApp(t)
	form := url.Values{"email": {"bademail"}, "username": {"a"}, "password": {"short"}}
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	app.Register(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Register POST invalid = %d, want 400", w.Code)
	}
}

func TestRegister_POST_Duplicate(t *testing.T) {
	app := newTestApp(t)
	form := url.Values{"email": {"a@b.com"}, "username": {"alice"}, "password": {"password123"}}
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		app.Register(w, req)
		if i == 1 && w.Code != http.StatusConflict {
			t.Errorf("Register duplicate = %d, want 409", w.Code)
		}
	}
}

func TestRegister_MethodNotAllowed(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodPut, "/register", nil)
	w := httptest.NewRecorder()
	app.Register(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Register PUT = %d, want 405", w.Code)
	}
}

func TestLogin_GET(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()
	app.Login(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Login GET = %d, want 200", w.Code)
	}
}

func TestLogin_POST_Success(t *testing.T) {
	app := newTestApp(t)
	// Register first.
	form := url.Values{"email": {"a@b.com"}, "username": {"alice"}, "password": {"password123"}}
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	app.Register(httptest.NewRecorder(), req)

	// Now login.
	form = url.Values{"email": {"a@b.com"}, "password": {"password123"}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	app.Login(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("Login POST success = %d, want 303", w.Code)
	}
	if w.Header().Get("Set-Cookie") == "" {
		t.Error("Login POST should set a session cookie")
	}
}

func TestLogin_POST_WrongPassword(t *testing.T) {
	app := newTestApp(t)
	form := url.Values{"email": {"a@b.com"}, "username": {"alice"}, "password": {"password123"}}
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	app.Register(httptest.NewRecorder(), req)

	form = url.Values{"email": {"a@b.com"}, "password": {"wrongpassword"}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	app.Login(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Login wrong password = %d, want 401", w.Code)
	}
}

func TestLogin_POST_UnknownEmail(t *testing.T) {
	app := newTestApp(t)
	form := url.Values{"email": {"nobody@b.com"}, "password": {"password123"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	app.Login(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Login unknown email = %d, want 401", w.Code)
	}
}

func TestOAuth_GET_DevFallback(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/auth/github", nil)
	w := httptest.NewRecorder()
	app.OAuth(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("OAuth dev GET = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "GitHub Login") {
		t.Error("OAuth dev GET should render provider login page")
	}
}

func TestOAuth_POST_GitHubDevLogin(t *testing.T) {
	app := newTestApp(t)
	form := url.Values{"username": {"githubuser"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/github", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	app.OAuth(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("OAuth GitHub POST = %d, want 303", w.Code)
	}
	if w.Header().Get("Set-Cookie") == "" {
		t.Error("OAuth GitHub POST should set a session cookie")
	}
	if _, err := app.Repo.UserByEmail("github-githubuser@oauth.local"); err != nil {
		t.Fatalf("OAuth GitHub user not created: %v", err)
	}
}

func TestOAuth_POST_GoogleDevLogin(t *testing.T) {
	app := newTestApp(t)
	form := url.Values{"username": {"googleuser"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/google", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	app.OAuth(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("OAuth Google POST = %d, want 303", w.Code)
	}
	if _, err := app.Repo.UserByEmail("google-googleuser@oauth.local"); err != nil {
		t.Fatalf("OAuth Google user not created: %v", err)
	}
}

func TestLogout_POST(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	w := httptest.NewRecorder()
	app.Logout(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("Logout POST = %d, want 303", w.Code)
	}
}

func TestLogout_MethodNotAllowed(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	w := httptest.NewRecorder()
	app.Logout(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Logout GET = %d, want 405", w.Code)
	}
}

// sessionCookie registers a user, logs in, and returns the session cookie.
func sessionCookie(t *testing.T, app *App) *http.Cookie {
	t.Helper()
	form := url.Values{"email": {"a@b.com"}, "username": {"alice"}, "password": {"password123"}}
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	app.Register(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			return c
		}
	}
	t.Fatal("no session cookie after register")
	return nil
}

func TestCreatePost_GET(t *testing.T) {
	app := newTestApp(t)
	cookie := sessionCookie(t, app)
	req := httptest.NewRequest(http.MethodGet, "/post/create", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	app.CreatePost(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("CreatePost GET = %d, want 200", w.Code)
	}
}

func TestCreatePost_GET_Unauthenticated(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/post/create", nil)
	w := httptest.NewRecorder()
	app.CreatePost(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("CreatePost unauthenticated = %d, want 401", w.Code)
	}
}

func TestCreatePost_POST_Success(t *testing.T) {
	app := newTestApp(t)
	cookie := sessionCookie(t, app)
	cats, _ := app.Repo.Categories()

	form := url.Values{"title": {"My Post"}, "content": {"Hello world"}, "categories": {strconv.Itoa(cats[0].ID)}}
	req := httptest.NewRequest(http.MethodPost, "/post/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	app.CreatePost(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("CreatePost POST = %d, want 303", w.Code)
	}
}

func TestCreatePost_POST_WithAllowedImages(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		ext  string
	}{
		{"png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}, ".png"},
		{"jpeg", []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F', 0, 0xff, 0xd9}, ".jpg"},
		{"gif", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), ".gif"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t)
			t.Setenv("UPLOAD_DIR", t.TempDir())
			t.Setenv("UPLOAD_URL_PREFIX", "/static/uploads")
			cookie := sessionCookie(t, app)
			cats, _ := app.Repo.Categories()

			req := multipartPostRequest(t, "/post/create", map[string]string{
				"title":      "Image post " + tc.name,
				"content":    "image body",
				"categories": strconv.Itoa(cats[0].ID),
			}, "image", "image"+tc.ext, tc.data)
			req.AddCookie(cookie)
			w := httptest.NewRecorder()
			app.CreatePost(w, req)
			if w.Code != http.StatusSeeOther {
				t.Fatalf("CreatePost with %s = %d, want 303", tc.name, w.Code)
			}
			posts, err := app.Repo.Posts("", 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(posts) != 1 || !strings.HasSuffix(posts[0].ImagePath, tc.ext) {
				t.Fatalf("expected saved %s image path, got posts=%v", tc.ext, posts)
			}
			req = httptest.NewRequest(http.MethodGet, "/post/1", nil)
			w = httptest.NewRecorder()
			app.PostRouter(w, req)
			if !strings.Contains(w.Body.String(), posts[0].ImagePath) {
				t.Fatal("post page should show the saved image after navigation")
			}
		})
	}
}

func TestCreatePost_POST_ImageTooLarge(t *testing.T) {
	app := newTestApp(t)
	t.Setenv("UPLOAD_DIR", t.TempDir())
	cookie := sessionCookie(t, app)
	cats, _ := app.Repo.Categories()
	large := make([]byte, maxPostImageBytes+1)
	copy(large, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})

	req := multipartPostRequest(t, "/post/create", map[string]string{
		"title":      "Large image post",
		"content":    "too large",
		"categories": strconv.Itoa(cats[0].ID),
	}, "image", "large.png", large)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	app.CreatePost(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreatePost with large image = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "20MB") {
		t.Fatal("large image response should warn about the 20MB limit")
	}
}

func TestCreatePost_POST_NoCategory(t *testing.T) {
	app := newTestApp(t)
	cookie := sessionCookie(t, app)
	form := url.Values{"title": {"My Post"}, "content": {"Hello world"}}
	req := httptest.NewRequest(http.MethodPost, "/post/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	app.CreatePost(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("CreatePost no category = %d, want 400", w.Code)
	}
}

func TestShowPost(t *testing.T) {
	app := newTestApp(t)
	cookie := sessionCookie(t, app)
	cats, _ := app.Repo.Categories()

	form := url.Values{"title": {"Post"}, "content": {"Body"}, "categories": {strconv.Itoa(cats[0].ID)}}
	req := httptest.NewRequest(http.MethodPost, "/post/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	app.CreatePost(httptest.NewRecorder(), req)

	req = httptest.NewRequest(http.MethodGet, "/post/1", nil)
	w := httptest.NewRecorder()
	app.PostRouter(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("ShowPost = %d, want 200", w.Code)
	}
}

func TestShowPost_NotFound(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/post/9999", nil)
	w := httptest.NewRecorder()
	app.PostRouter(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("ShowPost not found = %d, want 404", w.Code)
	}
}

func TestPostRouter_InvalidID(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/post/abc", nil)
	w := httptest.NewRecorder()
	app.PostRouter(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("PostRouter invalid id = %d, want 404", w.Code)
	}
}

func TestAddComment(t *testing.T) {
	app := newTestApp(t)
	cookie := sessionCookie(t, app)
	cats, _ := app.Repo.Categories()

	form := url.Values{"title": {"Post"}, "content": {"Body"}, "categories": {strconv.Itoa(cats[0].ID)}}
	req := httptest.NewRequest(http.MethodPost, "/post/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	app.CreatePost(httptest.NewRecorder(), req)

	form = url.Values{"content": {"Great post!"}}
	req = httptest.NewRequest(http.MethodPost, "/post/1/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	app.PostRouter(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("AddComment = %d, want 303", w.Code)
	}
}

func TestReactPost_Handler(t *testing.T) {
	app := newTestApp(t)
	cookie := sessionCookie(t, app)
	cats, _ := app.Repo.Categories()

	form := url.Values{"title": {"Post"}, "content": {"Body"}, "categories": {strconv.Itoa(cats[0].ID)}}
	req := httptest.NewRequest(http.MethodPost, "/post/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	app.CreatePost(httptest.NewRecorder(), req)

	form = url.Values{"value": {"1"}}
	req = httptest.NewRequest(http.MethodPost, "/post/1/react", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	app.PostRouter(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("ReactPost = %d, want 303", w.Code)
	}
}

func TestCommentRouter_NotFound(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/comment/1/react", nil)
	w := httptest.NewRecorder()
	app.CommentRouter(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("CommentRouter GET = %d, want 404", w.Code)
	}
}

func TestRoutes(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(app.Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / = %d, want 200", resp.StatusCode)
	}
}

func TestHome_FilterLiked_Unauthenticated(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/?type=liked", nil)
	w := httptest.NewRecorder()
	app.Home(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("Home liked unauthenticated = %d, want 303", w.Code)
	}
}

func TestHome_FilterCreated_Unauthenticated(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/?type=created", nil)
	w := httptest.NewRecorder()
	app.Home(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("Home created unauthenticated = %d, want 303", w.Code)
	}
}

func TestHome_FilterCategory(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/?category=1", nil)
	w := httptest.NewRecorder()
	app.Home(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Home category filter = %d, want 200", w.Code)
	}
}

func TestCreatePost_POST_EmptyTitle(t *testing.T) {
	app := newTestApp(t)
	cookie := sessionCookie(t, app)
	form := url.Values{"title": {""}, "content": {"body"}}
	req := httptest.NewRequest(http.MethodPost, "/post/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	app.CreatePost(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("CreatePost empty title = %d, want 400", w.Code)
	}
}

func TestCreatePost_MethodNotAllowed(t *testing.T) {
	app := newTestApp(t)
	cookie := sessionCookie(t, app)
	req := httptest.NewRequest(http.MethodPut, "/post/create", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	app.CreatePost(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("CreatePost PUT = %d, want 405", w.Code)
	}
}

func TestAddComment_Empty(t *testing.T) {
	app := newTestApp(t)
	cookie := sessionCookie(t, app)
	cats, _ := app.Repo.Categories()
	form := url.Values{"title": {"Post"}, "content": {"Body"}, "categories": {strconv.Itoa(cats[0].ID)}}
	req := httptest.NewRequest(http.MethodPost, "/post/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	app.CreatePost(httptest.NewRecorder(), req)

	form = url.Values{"content": {""}}
	req = httptest.NewRequest(http.MethodPost, "/post/1/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	app.PostRouter(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("AddComment empty = %d, want 400", w.Code)
	}
}

func TestAddComment_Unauthenticated(t *testing.T) {
	app := newTestApp(t)
	cookie := sessionCookie(t, app)
	cats, _ := app.Repo.Categories()
	form := url.Values{"title": {"Post"}, "content": {"Body"}, "categories": {strconv.Itoa(cats[0].ID)}}
	req := httptest.NewRequest(http.MethodPost, "/post/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	app.CreatePost(httptest.NewRecorder(), req)

	form = url.Values{"content": {"hello"}}
	req = httptest.NewRequest(http.MethodPost, "/post/1/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	app.PostRouter(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("AddComment unauthenticated = %d, want 401", w.Code)
	}
}

func TestReactComment_Handler(t *testing.T) {
	app := newTestApp(t)
	cookie := sessionCookie(t, app)
	cats, _ := app.Repo.Categories()

	form := url.Values{"title": {"Post"}, "content": {"Body"}, "categories": {strconv.Itoa(cats[0].ID)}}
	req := httptest.NewRequest(http.MethodPost, "/post/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	app.CreatePost(httptest.NewRecorder(), req)

	form = url.Values{"content": {"a comment"}}
	req = httptest.NewRequest(http.MethodPost, "/post/1/comment", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	app.PostRouter(httptest.NewRecorder(), req)

	form = url.Values{"value": {"1"}}
	req = httptest.NewRequest(http.MethodPost, "/comment/1/react", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	app.CommentRouter(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("ReactComment = %d, want 303", w.Code)
	}
}

func TestPostRouter_UnknownAction(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/post/1/unknown", nil)
	w := httptest.NewRecorder()
	app.PostRouter(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("PostRouter unknown action = %d, want 404", w.Code)
	}
}

func multipartPostRequest(t *testing.T, target string, fields map[string]string, fileField, filename string, fileData []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile(fileField, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(fileData); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

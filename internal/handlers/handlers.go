// Package handlers implements all HTTP handlers and routing for the forum.
package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"forum/internal/auth"
	"forum/internal/models"
	"forum/internal/repository"

	"github.com/gofrs/uuid/v5"
)

// App holds the shared dependencies for all HTTP handlers.
type App struct {
	Repo *repository.Repository
	Tpl  *template.Template
}

// ViewData is the template data passed to every page render.
type ViewData struct {
	Title      string
	User       *models.User
	Error      string
	Posts      []models.Post
	Post       models.Post
	Comments   []models.Comment
	Categories []models.Category
}

const maxPostImageBytes int64 = 20 << 20

// New parses all HTML templates and returns a ready-to-use App.
func New(repo *repository.Repository) (*App, error) {
	return NewWithTemplates(repo, "templates/*.html")
}

// NewWithTemplates is like New but accepts a custom template glob path (used in tests).
func NewWithTemplates(repo *repository.Repository, glob string) (*App, error) {
	funcs := template.FuncMap{"eq": func(a, b any) bool { return a == b }}
	tpl, err := template.New("").Funcs(funcs).ParseGlob(glob)
	if err != nil {
		return nil, err
	}
	return &App{Repo: repo, Tpl: tpl}, nil
}

// currentUser reads the session cookie and returns the logged-in user, or nil.
func (a *App) currentUser(r *http.Request) (*models.User, error) {
	c, err := r.Cookie("session_id")
	if err != nil {
		return nil, err
	}
	u, err := a.Repo.UserBySession(c.Value)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// render executes a named template, auto-filling User and Categories if not set.
func (a *App) render(w http.ResponseWriter, r *http.Request, name string, status int, data ViewData) {
	if data.User == nil {
		data.User, _ = a.currentUser(r)
	}
	if data.Categories == nil {
		data.Categories, _ = a.Repo.Categories()
	}
	w.WriteHeader(status)
	if err := a.Tpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// Routes registers all URL patterns and returns the root handler with middleware.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(envOrDefault("UPLOAD_DIR", filepath.Join("static", "uploads"))))))
	mux.HandleFunc("/", a.Home)
	mux.HandleFunc("/register", a.Register)
	mux.HandleFunc("/login", a.Login)
	mux.HandleFunc("/auth/", a.OAuth)
	mux.HandleFunc("/logout", a.Logout)
	mux.HandleFunc("/post/create", a.CreatePost)
	mux.HandleFunc("/post/", a.PostRouter)
	mux.HandleFunc("/comment/", a.CommentRouter)
	return recovery(methodOverride(mux))
}

// Home renders the post list, optionally filtered by category, "created" or "liked".
func (a *App) Home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	u, _ := a.currentUser(r)
	filter := r.URL.Query().Get("type")
	catID, _ := strconv.Atoi(r.URL.Query().Get("category"))
	if (filter == "created" || filter == "liked") && u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	uid := 0
	if u != nil {
		uid = u.ID
	}
	posts, err := a.Repo.Posts(filter, catID, uid)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	a.render(w, r, "home.html", 200, ViewData{Title: "Forum", User: u, Posts: posts})
}

// Register handles user sign-up and auto-logs in on success.
func (a *App) Register(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.render(w, r, "register.html", 200, ViewData{Title: "Register"})
		return
	case http.MethodPost:
	default:
		http.Error(w, "method not allowed", 405)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if !auth.ValidEmail(email) || !auth.ValidUsername(username) || !auth.ValidPassword(password) {
		a.render(w, r, "register.html", 400, ViewData{Title: "Register", Error: "Use a valid email, username of 3-30 characters, and password of at least 8 characters."})
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		log.Println("HashPassword error:", err)
		http.Error(w, "server error", 500)
		return
	}
	if err := a.Repo.CreateUser(email, username, hash); err != nil {
		log.Println("CreateUser error:", err)
		a.render(w, r, "register.html", 409, ViewData{Title: "Register", Error: "Email or username is already taken."})
		return
	}
	u, err := a.Repo.UserByEmail(email)
	if err != nil {
		log.Println("UserByEmail after register error:", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	sid, err := uuid.NewV4()
	if err != nil {
		http.Error(w, "server error", 500)
		return
	}
	expires := time.Now().Add(24 * time.Hour)
	_ = a.Repo.CreateSession(sid.String(), u.ID, expires)
	http.SetCookie(w, &http.Cookie{Name: "session_id", Value: sid.String(), Path: "/", Expires: expires, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Login authenticates a user by email and password and creates a session cookie.
func (a *App) Login(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.render(w, r, "login.html", 200, ViewData{Title: "Login"})
		return
	case http.MethodPost:
	default:
		http.Error(w, "method not allowed", 405)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	u, err := a.Repo.UserByEmail(email)
	if err != nil {
		log.Println("UserByEmail error:", err)
		a.render(w, r, "login.html", 401, ViewData{Title: "Login", Error: "Invalid email or password."})
		return
	}
	if !auth.CheckPassword(u.PasswordHash, password) {
		log.Println("CheckPassword failed for:", email)
		a.render(w, r, "login.html", 401, ViewData{Title: "Login", Error: "Invalid email or password."})
		return
	}
	sid, err := uuid.NewV4()
	if err != nil {
		http.Error(w, "server error", 500)
		return
	}
	expires := time.Now().Add(24 * time.Hour)
	if err := a.Repo.CreateSession(sid.String(), u.ID, expires); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "session_id", Value: sid.String(), Path: "/", Expires: expires, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// OAuth handles GitHub/Google login. Real OAuth is used when client credentials
// are configured; otherwise a local development fallback is shown.
func (a *App) OAuth(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/auth/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	provider := parts[0]
	if provider != "github" && provider != "google" {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 2 && parts[1] == "callback" {
		a.OAuthCallback(w, r, provider)
		return
	}
	if r.Method == http.MethodPost {
		a.OAuthDevLogin(w, r, provider)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := oauthConfig(provider)
	if !cfg.Configured() {
		a.render(w, r, "oauth_dev.html", http.StatusOK, ViewData{Title: providerLabel(provider) + " Login"})
		return
	}
	state, err := uuid.NewV4()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: state.String(), Path: "/", MaxAge: 300, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, cfg.AuthURL(state.String()), http.StatusSeeOther)
}

// OAuthCallback completes a real GitHub/Google OAuth login.
func (a *App) OAuthCallback(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		a.render(w, r, "login.html", http.StatusBadRequest, ViewData{Title: "Login", Error: "Invalid authentication state."})
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		a.render(w, r, "login.html", http.StatusBadRequest, ViewData{Title: "Login", Error: "Authentication was cancelled or failed."})
		return
	}
	profile, err := fetchOAuthProfile(provider, code, r)
	if err != nil {
		log.Println("OAuth error:", err)
		a.render(w, r, "login.html", http.StatusBadRequest, ViewData{Title: "Login", Error: "Could not authenticate with " + providerLabel(provider) + "."})
		return
	}
	a.finishOAuthLogin(w, r, provider, profile)
}

// OAuthDevLogin is a local fallback for testing without provider credentials.
func (a *App) OAuthDevLogin(w http.ResponseWriter, r *http.Request, provider string) {
	name := strings.TrimSpace(r.FormValue("username"))
	if name == "" {
		name = provider + "user"
	}
	profile := oauthProfile{
		ID:       "local-" + sanitizeUsername(name),
		Email:    provider + "-" + sanitizeUsername(name) + "@oauth.local",
		Username: name,
	}
	a.finishOAuthLogin(w, r, provider, profile)
}

func (a *App) finishOAuthLogin(w http.ResponseWriter, r *http.Request, provider string, profile oauthProfile) {
	username := sanitizeUsername(profile.Username)
	if username == "" {
		username = provider + "user"
	}
	email := strings.TrimSpace(profile.Email)
	if email == "" {
		email = provider + "-" + sanitizeUsername(profile.ID) + "@oauth.local"
	}
	secret, err := uuid.NewV4()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	hash, err := auth.HashPassword(provider + ":" + secret.String())
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	u, err := a.Repo.FindOrCreateOAuthUser(email, uniqueOAuthUsername(provider, username), hash)
	if err != nil {
		log.Println("FindOrCreateOAuthUser error:", err)
		a.render(w, r, "login.html", http.StatusConflict, ViewData{Title: "Login", Error: "Could not create OAuth user."})
		return
	}
	if err := a.createSession(w, u.ID); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout deletes the session and clears the session cookie.
func (a *App) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if c, err := r.Cookie("session_id"); err == nil {
		_ = a.Repo.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "session_id", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) createSession(w http.ResponseWriter, userID int) error {
	sid, err := uuid.NewV4()
	if err != nil {
		return err
	}
	expires := time.Now().Add(24 * time.Hour)
	if err := a.Repo.CreateSession(sid.String(), userID, expires); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "session_id", Value: sid.String(), Path: "/", Expires: expires, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	return nil
}

// CreatePost handles new post creation (GET shows form, POST saves).
func (a *App) CreatePost(w http.ResponseWriter, r *http.Request) {
	u, err := a.currentUser(r)
	if err != nil {
		http.Error(w, "login required", 401)
		return
	}
	if r.Method == http.MethodGet {
		a.render(w, r, "create_post.html", 200, ViewData{Title: "Create post", User: u})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPostImageBytes+(1<<20))
	if err := parsePostForm(r); err != nil {
		a.render(w, r, "create_post.html", 400, ViewData{Title: "Create post", User: u, Error: "Image must be 20MB or smaller."})
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	content := strings.TrimSpace(r.FormValue("content"))
	if title == "" || content == "" {
		a.render(w, r, "create_post.html", 400, ViewData{Title: "Create post", User: u, Error: "Title and content are required."})
		return
	}
	var catIDs []int
	for _, v := range r.Form["categories"] {
		id, _ := strconv.Atoi(v)
		if id > 0 {
			catIDs = append(catIDs, id)
		}
	}
	if len(catIDs) == 0 {
		a.render(w, r, "create_post.html", 400, ViewData{Title: "Create post", User: u, Error: "Choose at least one category."})
		return
	}
	imagePath, err := savePostImage(r)
	if err != nil {
		a.render(w, r, "create_post.html", 400, ViewData{Title: "Create post", User: u, Error: err.Error()})
		return
	}
	if err := a.Repo.CreatePostWithImage(u.ID, title, content, imagePath, catIDs); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// PostRouter dispatches /post/{id}, /post/{id}/comment and /post/{id}/react.
func (a *App) PostRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/post/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	id, err := strconv.Atoi(parts[0])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		a.ShowPost(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "comment" && r.Method == http.MethodPost {
		a.AddComment(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "react" && r.Method == http.MethodPost {
		a.ReactPost(w, r, id)
		return
	}
	http.Error(w, "not found", 404)
}

// ShowPost renders a single post with its comments.
func (a *App) ShowPost(w http.ResponseWriter, r *http.Request, id int) {
	u, _ := a.currentUser(r)
	uid := 0
	if u != nil {
		uid = u.ID
	}
	p, err := a.Repo.Post(id, uid)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	comments, err := a.Repo.Comments(id, uid)
	if err != nil {
		http.Error(w, "database error", 500)
		return
	}
	a.render(w, r, "post.html", 200, ViewData{Title: p.Title, User: u, Post: p, Comments: comments})
}

// AddComment saves a new comment on a post.
func (a *App) AddComment(w http.ResponseWriter, r *http.Request, postID int) {
	u, err := a.currentUser(r)
	if err != nil {
		http.Error(w, "login required", 401)
		return
	}
	content := strings.TrimSpace(r.FormValue("content"))
	if content == "" {
		http.Error(w, "empty comment", 400)
		return
	}
	if err := a.Repo.CreateComment(postID, u.ID, content); err != nil {
		http.Error(w, "database error", 500)
		return
	}
	http.Redirect(w, r, "/post/"+strconv.Itoa(postID), http.StatusSeeOther)
}

// ReactPost records a like/dislike on a post from the logged-in user.
func (a *App) ReactPost(w http.ResponseWriter, r *http.Request, postID int) {
	u, err := a.currentUser(r)
	if err != nil {
		http.Error(w, "login required", 401)
		return
	}
	r.ParseForm()
	value, _ := strconv.Atoi(r.FormValue("value"))
	if err := a.Repo.ReactPost(u.ID, postID, value); err != nil {
		http.Error(w, "bad reaction", 400)
		return
	}
	ref := r.Referer()
	if ref == "" {
		ref = "/post/" + strconv.Itoa(postID)
	}
	http.Redirect(w, r, ref, http.StatusSeeOther)
}

// CommentRouter dispatches /comment/{id}/react.
func (a *App) CommentRouter(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/comment/"), "/"), "/")
	if len(parts) == 2 && parts[1] == "react" && r.Method == http.MethodPost {
		id, _ := strconv.Atoi(parts[0])
		a.ReactComment(w, r, id)
		return
	}
	http.NotFound(w, r)
}

// ReactComment records a like/dislike on a comment from the logged-in user.
func (a *App) ReactComment(w http.ResponseWriter, r *http.Request, commentID int) {
	u, err := a.currentUser(r)
	if err != nil {
		http.Error(w, "login required", 401)
		return
	}
	r.ParseForm()
	value, _ := strconv.Atoi(r.FormValue("value"))
	if err := a.Repo.ReactComment(u.ID, commentID, value); err != nil {
		http.Error(w, "bad reaction", 400)
		return
	}
	ref := r.Referer()
	if ref == "" {
		ref = "/"
	}
	http.Redirect(w, r, ref, http.StatusSeeOther)
}

// methodOverride is a middleware placeholder for future HTTP method tunnelling.
func methodOverride(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(w, r) })
}

// recovery is a middleware that catches panics and returns a 500 response.
func recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				http.Error(w, "internal server error", 500)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type oauthProvider struct {
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	AuthEndpoint  string
	TokenEndpoint string
	ProfileURL    string
	Scopes        []string
}

type oauthProfile struct {
	ID       string
	Email    string
	Username string
}

func oauthConfig(provider string) oauthProvider {
	baseURL := envOrDefault("APP_URL", "http://localhost:8080")
	switch provider {
	case "github":
		return oauthProvider{
			ClientID:      os.Getenv("GITHUB_CLIENT_ID"),
			ClientSecret:  os.Getenv("GITHUB_CLIENT_SECRET"),
			RedirectURL:   strings.TrimRight(baseURL, "/") + "/auth/github/callback",
			AuthEndpoint:  "https://github.com/login/oauth/authorize",
			TokenEndpoint: "https://github.com/login/oauth/access_token",
			ProfileURL:    "https://api.github.com/user",
			Scopes:        []string{"read:user", "user:email"},
		}
	case "google":
		return oauthProvider{
			ClientID:      os.Getenv("GOOGLE_CLIENT_ID"),
			ClientSecret:  os.Getenv("GOOGLE_CLIENT_SECRET"),
			RedirectURL:   strings.TrimRight(baseURL, "/") + "/auth/google/callback",
			AuthEndpoint:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenEndpoint: "https://oauth2.googleapis.com/token",
			ProfileURL:    "https://www.googleapis.com/oauth2/v2/userinfo",
			Scopes:        []string{"openid", "email", "profile"},
		}
	default:
		return oauthProvider{}
	}
}

func (p oauthProvider) Configured() bool {
	return p.ClientID != "" && p.ClientSecret != ""
}

func (p oauthProvider) AuthURL(state string) string {
	q := url.Values{}
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", p.RedirectURL)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(p.Scopes, " "))
	q.Set("state", state)
	return p.AuthEndpoint + "?" + q.Encode()
}

func fetchOAuthProfile(provider, code string, r *http.Request) (oauthProfile, error) {
	cfg := oauthConfig(provider)
	token, err := exchangeOAuthCode(cfg, code)
	if err != nil {
		return oauthProfile{}, err
	}
	req, err := http.NewRequest(http.MethodGet, cfg.ProfileURL, nil)
	if err != nil {
		return oauthProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return oauthProfile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return oauthProfile{}, errors.New("profile request failed")
	}
	if provider == "github" {
		return decodeGitHubProfile(resp.Body)
	}
	return decodeGoogleProfile(resp.Body)
}

func exchangeOAuthCode(cfg oauthProvider, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", cfg.RedirectURL)
	form.Set("grant_type", "authorization_code")
	req, err := http.NewRequest(http.MethodPost, cfg.TokenEndpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("token request failed")
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("missing access token")
	}
	return out.AccessToken, nil
}

func decodeGitHubProfile(body io.Reader) (oauthProfile, error) {
	var data struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(body).Decode(&data); err != nil {
		return oauthProfile{}, err
	}
	email := data.Email
	if email == "" {
		email = "github-" + strconv.FormatInt(data.ID, 10) + "@oauth.local"
	}
	username := data.Login
	if username == "" {
		username = data.Name
	}
	return oauthProfile{ID: strconv.FormatInt(data.ID, 10), Email: email, Username: username}, nil
}

func decodeGoogleProfile(body io.Reader) (oauthProfile, error) {
	var data struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(body).Decode(&data); err != nil {
		return oauthProfile{}, err
	}
	return oauthProfile{ID: data.ID, Email: data.Email, Username: data.Name}, nil
}

func sanitizeUsername(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 20 {
		out = out[:20]
	}
	if len(out) < 3 {
		out = out + "user"
	}
	return out
}

func uniqueOAuthUsername(provider, username string) string {
	out := provider + "-" + sanitizeUsername(username)
	if len(out) > 30 {
		return out[:30]
	}
	return out
}

func envOrDefault(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func providerLabel(provider string) string {
	switch provider {
	case "github":
		return "GitHub"
	case "google":
		return "Google"
	default:
		return provider
	}
}

func parsePostForm(r *http.Request) error {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return r.ParseMultipartForm(maxPostImageBytes)
	}
	return r.ParseForm()
}

func savePostImage(r *http.Request) (string, error) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return "", nil
	}
	file, header, err := r.FormFile("image")
	if errors.Is(err, http.ErrMissingFile) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	if header.Size > maxPostImageBytes {
		return "", errors.New("Image must be 20MB or smaller.")
	}
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}
	contentType := http.DetectContentType(buf[:n])
	ext, ok := imageExtension(contentType)
	if !ok {
		return "", errors.New("Only PNG, JPEG, and GIF images are allowed.")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	uploadDir := envOrDefault("UPLOAD_DIR", filepath.Join("static", "uploads"))
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return "", err
	}
	id, err := uuid.NewV4()
	if err != nil {
		return "", err
	}
	filename := id.String() + ext
	dstPath := filepath.Join(uploadDir, filename)
	dst, err := os.Create(dstPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()
	limited := &io.LimitedReader{R: file, N: maxPostImageBytes + 1}
	written, err := io.Copy(dst, limited)
	if err != nil {
		return "", err
	}
	if written > maxPostImageBytes {
		_ = os.Remove(dstPath)
		return "", errors.New("Image must be 20MB or smaller.")
	}
	return strings.TrimRight(envOrDefault("UPLOAD_URL_PREFIX", "/static/uploads"), "/") + "/" + filename, nil
}

func imageExtension(contentType string) (string, bool) {
	switch contentType {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/gif":
		return ".gif", true
	default:
		return "", false
	}
}

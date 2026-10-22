package server

import (
	"context"
	"embed"
	"html/template"
	"net/http"
	"strings"
	"time"

	"logintoreadnormal/internal/models"
)

//go:embed templates/*.html
var templateFS embed.FS

// templates holds two kinds of parsed template sets:
//   - standalone: full self-contained documents (no external references), used
//     for the sterile primary page, the one-time credentials page, the unlock
//     form and the "expired" page;
//   - pages: app pages that share layout_app.html ("layout" + "content").
type templates struct {
	standalone map[string]*template.Template
	pages      map[string]*template.Template
}

var standaloneNames = []string{"primary_login", "credentials", "link_unlock", "expired"}
var layoutNames = []string{"dashboard", "paste_new", "paste_view", "users", "error"}

// templateFuncs are helpers available inside templates.
var templateFuncs = template.FuncMap{
	"join": strings.Join,
}

func loadTemplates() (*templates, error) {
	t := &templates{
		standalone: make(map[string]*template.Template, len(standaloneNames)),
		pages:      make(map[string]*template.Template, len(layoutNames)),
	}
	for _, name := range standaloneNames {
		tpl, err := template.New(name).Funcs(templateFuncs).ParseFS(templateFS, "templates/"+name+".html")
		if err != nil {
			return nil, err
		}
		t.standalone[name] = tpl
	}
	for _, name := range layoutNames {
		tpl, err := template.New("layout").Funcs(templateFuncs).ParseFS(templateFS, "templates/layout_app.html", "templates/"+name+".html")
		if err != nil {
			return nil, err
		}
		t.pages[name] = tpl
	}
	return t, nil
}

// pasteView is a paste prepared for rendering.
type pasteView struct {
	ID         int64
	Title      string
	Author     string
	Body       string
	IsDeleted  bool
	Audience   string
	IsSpecific bool     // audience == "specific"
	Recipients []string // recipient logins; only filled for the admin
	CreatedUTC string   // always-visible UTC+0 value
	CreatedISO string   // machine-readable ISO for the local-time script
	CanDelete  bool
}

func newPasteView(p models.Paste, canDelete bool, recipients []string) pasteView {
	body := ""
	if p.IsDeleted {
		body = "[Данные удалены]"
	} else if p.Body.Valid {
		body = p.Body.String
	}
	aud := p.Audience
	switch p.Audience {
	case "all":
		aud = "всем"
	case "specific":
		aud = "конкретным пользователям"
	case "admin_only":
		aud = "только администратору"
	}
	return pasteView{
		ID:         p.ID,
		Title:      p.Title,
		Author:     p.AuthorName,
		Body:       body,
		IsDeleted:  p.IsDeleted,
		Audience:   aud,
		IsSpecific: p.Audience == "specific",
		Recipients: recipients,
		CreatedUTC: models.FormatDisplay(p.CreatedAt) + " UTC",
		CreatedISO: p.CreatedAt.Format(models.TimeLayout),
		CanDelete:  canDelete,
	}
}

// pasteViewsFor prepares many pastes for rendering. For the admin it also fills
// in the recipient logins of every "specific" paste, using a single extra query
// (instead of one query per paste).
func (s *Server) pasteViewsFor(ctx context.Context, actor *models.Actor, pastes []models.Paste) ([]pasteView, error) {
	isAdmin := actor.IsAdmin()

	var recipients map[int64][]string
	if isAdmin {
		ids := make([]int64, 0, len(pastes))
		for _, p := range pastes {
			if p.Audience == "specific" {
				ids = append(ids, p.ID)
			}
		}
		if len(ids) > 0 {
			m, err := s.repo.RecipientsForPastes(ctx, ids)
			if err != nil {
				return nil, err
			}
			recipients = m
		}
	}

	views := make([]pasteView, 0, len(pastes))
	for _, p := range pastes {
		views = append(views, newPasteView(p, isAdmin, recipients[p.ID]))
	}
	return views, nil
}

// pasteViewFor prepares a single paste for rendering (same rules as above).
func (s *Server) pasteViewFor(ctx context.Context, actor *models.Actor, p models.Paste) (pasteView, error) {
	if !actor.IsAdmin() || p.Audience != "specific" {
		return newPasteView(p, actor.IsAdmin(), nil), nil
	}
	m, err := s.repo.RecipientsForPastes(ctx, []int64{p.ID})
	if err != nil {
		return pasteView{}, err
	}
	return newPasteView(p, true, m[p.ID]), nil
}

// pageData is the common payload passed to templates.
type pageData struct {
	Title       string
	PrimaryPath string
	Base        string // "/l/<token>" for app pages
	CSRF        string
	Actor       *models.Actor
	IsAdmin     bool
	Users       []models.Actor
	Pastes      []pasteView
	Paste       *pasteView
	Counter     int
	ShowCounter bool
	Error       string
	Notice      string
	// credentials page
	LinkURL  string
	Password string
}

// base returns the interface prefix for a link token.
func base(token string) string { return "/l/" + token }

// render writes a page. Unknown page names fall back to the generic error page.
func (s *Server) render(w http.ResponseWriter, page string, data *pageData, status int) {
	if data == nil {
		data = &pageData{}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	if tpl, ok := s.tmpl.standalone[page]; ok {
		if err := tpl.ExecuteTemplate(w, page+".html", data); err != nil {
			// Nothing sensible to do once headers are sent.
			return
		}
		return
	}
	tpl, ok := s.tmpl.pages[page]
	if !ok {
		tpl = s.tmpl.pages["error"]
	}
	_ = tpl.ExecuteTemplate(w, "layout", data)
}

// renderError renders the generic in-app error page.
func (s *Server) renderError(w http.ResponseWriter, d *pageData, msg string, status int) {
	if d == nil {
		d = &pageData{}
	}
	d.Title = "Ошибка"
	d.Error = msg
	s.render(w, "error", d, status)
}

// nowUTC is a small indirection to keep handlers readable.
func nowUTC() time.Time { return time.Now().UTC() }

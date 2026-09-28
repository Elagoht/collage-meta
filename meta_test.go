package meta_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	meta "github.com/Elagoht/collage-meta"
	"github.com/Elagoht/collage/pkg/collage"
)

var published = time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

type view struct{}

func site(t *testing.T, opts meta.Options, config map[string]json.RawMessage) http.Handler {
	t.Helper()
	app, err := collage.New(&collage.Config{
		Server: collage.ServerConfig{Host: "localhost", Port: 3000},
		Template: collage.TemplateConfig{FS: fstest.MapFS{
			"t/layout.html":   {Data: []byte(`<html><head>{{hoist "head"}}</head><body>{{slot "content"}}</body></html>`)},
			"t/p.html":        {Data: []byte(`<main>page</main>`)},
			"t/override.html": {Data: []byte(`<main>override{{slot "inner"}}</main>`)},
		}, Root: "t"},
		Locale:       collage.LocaleConfig{Default: "en", Supported: []string{"en", "tr"}},
		Plugins:      []collage.Plugin{meta.New(opts)},
		PluginConfig: config,
	})
	if err != nil {
		t.Fatal(err)
	}
	layout := func() *collage.Fragment { return collage.NewFragment("layout", "layout.html").Build() }

	home := collage.NewPage("home").WithLayouts(layout()).
		WithContent(collage.NewFragment("home", "p.html").Build()).
		WithPath("en", "/").WithPath("tr", "/").Build()

	post := collage.NewPage("post").WithLayouts(layout()).
		WithContent(collage.NewFragment("post", "p.html").WithDataHandler(
			func(_ context.Context, rc *collage.RenderContext) (any, []string, error) { // any: DataHandlerFunc's own return type
				meta.Set(rc, meta.Page{
					Title:       `Hello <"world">`,
					Description: "A post & more",
					Image:       "/img/" + rc.Param("slug") + ".png",
					ImageAlt:    "A cover",
					Type:        meta.Article,
					Published:   published,
					Modified:    published.Add(time.Hour),
					Author:      "Ada",
				})
				return view{}, nil, nil
			}).Build()).
		WithPath("en", "/blog/{slug}").WithPath("tr", "/yazi/{slug}").Build()

	// Only in English, and a copy of another page: its canonical is set by hand,
	// and a fragment's own description beats Set's.
	only := collage.NewPage("only").WithLayouts(layout()).
		WithContent(collage.NewFragment("only", "override.html").WithDataHandler(
			func(_ context.Context, rc *collage.RenderContext) (any, []string, error) { // any: DataHandlerFunc's own return type
				meta.Set(rc, meta.Page{Canonical: "/blog/original", Description: "from Set", TwitterCard: meta.Summary})
				return view{}, nil, nil
			}).WithSlot("inner", false, false).WithSlotFragment("inner", collage.NewFragment("inner", "p.html").WithDataHandler(
			func(_ context.Context, rc *collage.RenderContext) (any, []string, error) { // any: DataHandlerFunc's own return type
				rc.HoistMeta("description", "from a fragment")
				return view{}, nil, nil
			}).Build()).Build()).
		WithPath("en", "/only").Build()

	for _, p := range []*collage.Page{home, post, only} {
		if err := app.RegisterPage(p); err != nil {
			t.Fatal(err)
		}
	}
	return app.Handler()
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func contains(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("head lacks %s\n%s", want, body)
		}
	}
}

func lacks(t *testing.T, body string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(body, u) {
			t.Errorf("head has %s\n%s", u, body)
		}
	}
}

var defaults = meta.Options{
	SiteName:     "The blog",
	BaseURL:      "https://example.com/",
	DefaultImage: "/static/share.png",
	TwitterSite:  "@example",
	Locales:      map[string]string{"en": "en_US"},
}

// A page that says nothing still gets the site's defaults, its canonical URL, and
// its translations.
func TestDefaults(t *testing.T) {
	body := get(site(t, defaults, nil), "/").Body.String()
	contains(t, body,
		`<meta property="og:site_name" content="The blog">`,
		`<meta property="og:type" content="website">`,
		`<link rel="canonical" href="https://example.com/">`,
		`<meta property="og:url" content="https://example.com/">`,
		`<meta property="og:locale" content="en_US">`,
		`<meta property="og:locale:alternate" content="tr">`,
		`<link rel="alternate" hreflang="en" href="https://example.com/">`,
		`<link rel="alternate" hreflang="tr" href="https://example.com/tr">`,
		`<link rel="alternate" hreflang="x-default" href="https://example.com/">`,
		`<meta property="og:image" content="https://example.com/static/share.png">`,
		`<meta name="twitter:card" content="summary_large_image">`,
		`<meta name="twitter:site" content="@example">`,
	)
	lacks(t, body, `og:title`, `name="description"`)
}

// What a page says wins over the defaults, tag by tag, escaped, in its own locale.
func TestSet(t *testing.T) {
	h := site(t, defaults, nil)
	body := get(h, "/tr/yazi/hello").Body.String()
	contains(t, body,
		`<meta property="og:title" content="Hello &lt;&#34;world&#34;&gt;">`,
		`<meta name="description" content="A post &amp; more">`,
		`<meta property="og:description" content="A post &amp; more">`,
		`<meta property="og:type" content="article">`,
		`<meta property="og:image" content="https://example.com/img/hello.png">`,
		`<meta property="og:image:alt" content="A cover">`,
		`<meta name="twitter:image:alt" content="A cover">`,
		`<meta property="article:published_time" content="2026-09-20T10:00:00Z">`,
		`<meta property="article:modified_time" content="2026-09-20T11:00:00Z">`,
		`<meta property="article:author" content="Ada">`,
		`<link rel="canonical" href="https://example.com/tr/yazi/hello">`,
		`<meta property="og:locale" content="tr">`,
		`<meta property="og:locale:alternate" content="en_US">`,
		`<link rel="alternate" hreflang="en" href="https://example.com/blog/hello">`,
		`<link rel="alternate" hreflang="tr" href="https://example.com/tr/yazi/hello">`,
		`<link rel="alternate" hreflang="x-default" href="https://example.com/blog/hello">`,
	)
	lacks(t, body, `share.png`, `content="website"`)
	if n := strings.Count(body, `property="og:image"`); n != 1 {
		t.Errorf("%d og:image tags, want 1", n)
	}
}

// A canonical set by hand replaces the worked-out one; a deeper fragment's own
// description wins over Set's; a page in one locale has no alternates.
func TestCanonicalAndSingleLocale(t *testing.T) {
	body := get(site(t, defaults, nil), "/only").Body.String()
	contains(t, body,
		`<link rel="canonical" href="https://example.com/blog/original">`,
		`<meta property="og:url" content="https://example.com/blog/original">`,
		`<meta name="twitter:card" content="summary">`,
		`<meta name="description" content="from a fragment">`,
		`<meta property="og:description" content="from Set">`,
	)
	lacks(t, body, `hreflang`, `og:locale:alternate`, `href="https://example.com/only"`)
	if n := strings.Count(body, `rel="canonical"`); n != 1 {
		t.Errorf("%d canonical links, want 1", n)
	}
}

// Configuration overlays the options; a site with no image has a summary card;
// NoAlternates leaves the hreflang links to the layout.
func TestConfiguration(t *testing.T) {
	config := map[string]json.RawMessage{meta.Name: json.RawMessage(`{"baseURL":"https://other.example","noAlternates":true,"defaultImage":""}`)}
	body := get(site(t, defaults, config), "/").Body.String()
	contains(t, body,
		`<link rel="canonical" href="https://other.example/">`,
		`<meta name="twitter:card" content="summary">`,
		`<meta property="og:locale:alternate" content="tr">`,
	)
	lacks(t, body, `hreflang`, `og:image`)
}

// A site without an absolute origin does not start.
func TestBaseURLRequired(t *testing.T) {
	for _, base := range []string{"", "example.com", "/", "ftp://example.com", "https://example.com/?a=1"} {
		opts := defaults
		opts.BaseURL = base
		if code := get(site(t, opts, nil), "/").Code; code != http.StatusServiceUnavailable {
			t.Errorf("BaseURL %q: %d, want 503", base, code)
		}
	}
}

// Without the plugin, Set still declares, and cannot make a path absolute.
func TestSetWithoutPlugin(t *testing.T) {
	meta.Set(nil, meta.Page{Title: "x"})
	app, err := collage.New(&collage.Config{
		Server: collage.ServerConfig{Host: "localhost", Port: 3000},
		Template: collage.TemplateConfig{FS: fstest.MapFS{
			"t/p.html": {Data: []byte(`<html><head>{{hoist "head"}}</head></html>`)},
		}, Root: "t"},
	})
	if err != nil {
		t.Fatal(err)
	}
	page := collage.NewPage("p").WithContent(collage.NewFragment("p", "p.html").WithDataHandler(
		func(_ context.Context, rc *collage.RenderContext) (any, []string, error) { // any: DataHandlerFunc's own return type
			meta.Set(rc, meta.Page{Image: "/a.png", Canonical: "https://example.com/p"})
			return view{}, nil, nil
		}).Build()).WithPath("en", "/").Build()
	if err := app.RegisterPage(page); err != nil {
		t.Fatal(err)
	}
	body := get(app.Handler(), "/").Body.String()
	contains(t, body, `<meta property="og:image" content="/a.png">`, `<link rel="canonical" href="https://example.com/p">`)
}

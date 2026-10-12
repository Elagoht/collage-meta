package meta_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	meta "github.com/Elagoht/collage-meta"
	"github.com/Elagoht/collage/pkg/collage"
	"github.com/Elagoht/collage/pkg/collagetest"
)

var published = time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

// layoutFragment is the layout every test page renders in: the Site's own.
func layoutFragment() *collage.Fragment { return collage.NewFragment("layout", "layout.html").Build() }

// pages are the three pages the tests read: a home page in two locales, a post
// that says what it is about, and an English-only copy of another page.
func pages() []collage.Registrable {
	home := collage.NewPage("home").WithLayouts(layoutFragment()).
		WithContent(collage.NewFragment("home", "p.html").Build()).
		WithPath("en", "/").WithPath("tr", "/").Build()

	post := collage.NewPage("post").WithLayouts(layoutFragment()).
		WithContent(collage.NewFragment("post", "p.html").WithData(collage.Effect(
			func(_ context.Context, rc *collage.RenderContext) error {
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
				return nil
			})).Build()).
		WithPath("en", "/blog/{slug}").WithPath("tr", "/yazi/{slug}").Build()

	// Only in English, and a copy of another page: its canonical is set by hand,
	// and a fragment's own description beats Set's.
	only := collage.NewPage("only").WithLayouts(layoutFragment()).
		WithContent(collage.NewFragment("only", "override.html").WithData(collage.Effect(
			func(_ context.Context, rc *collage.RenderContext) error {
				meta.Set(rc, meta.Page{Canonical: "/blog/original", Description: "from Set", TwitterCard: meta.Summary})
				return nil
			})).WithSlot("inner").WithSlotFragment("inner", collage.NewFragment("inner", "p.html").WithData(collage.Effect(
			func(_ context.Context, rc *collage.RenderContext) error {
				rc.HoistMeta("description", "from a fragment")
				return nil
			})).Build()).Build()).
		WithPath("en", "/only").Build()

	return []collage.Registrable{home, post, only}
}

// siteOptions builds the test site: English and Turkish, the given BaseURL, the
// plugin with opts, and the three pages.
func siteOptions(opts meta.Options, baseURL string, extra ...collagetest.SiteOption) []collagetest.SiteOption {
	return append([]collagetest.SiteOption{
		collagetest.WithoutHome(),
		collagetest.WithTemplates(map[string]string{
			"p.html":        `<main>page</main>`,
			"override.html": `<main>override{{slot "inner"}}</main>`,
		}),
		collagetest.WithRegister(pages()...),
		collagetest.WithConfig(func(c *collage.Config) {
			c.Locale = collage.LocaleConfig{Default: "en", Supported: []string{"en", "tr"}}
			c.BaseURL = baseURL
		}),
		collagetest.WithPlugins(meta.New(opts)),
	}, extra...)
}

func site(t *testing.T, opts meta.Options, extra ...collagetest.SiteOption) *collagetest.Site {
	t.Helper()
	return collagetest.NewSite(t, siteOptions(opts, "https://example.com", extra...)...)
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
	DefaultImage: "/static/share.png",
	TwitterSite:  "@example",
	Locales:      map[string]string{"en": "en_US"},
}

// A page that says nothing still gets the site's defaults, its canonical URL, and
// its translations.
func TestDefaults(t *testing.T) {
	body := site(t, defaults).Get("/").Body
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
	body := site(t, defaults).Get("/tr/yazi/hello").Body
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
	body := site(t, defaults).Get("/only").Body
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
	body := site(t, defaults,
		collagetest.WithPluginConfig(meta.Name, `{"noAlternates":true,"defaultImage":""}`),
	).Get("/").Body
	contains(t, body,
		`<link rel="canonical" href="https://example.com/">`,
		`<meta name="twitter:card" content="summary">`,
		`<meta property="og:locale:alternate" content="tr">`,
	)
	lacks(t, body, `hreflang`, `og:image`)
}

// The plugin's own baseURL key is gone (v0.3.0): URLs follow Config.BaseURL, and
// a configuration still naming the key stops the application.
func TestBaseURLKeyRemoved(t *testing.T) {
	_, err := collagetest.TrySite(t, siteOptions(defaults, "https://example.com",
		collagetest.WithPluginConfig(meta.Name, `{"baseURL":"https://other.example"}`))...)
	if err == nil || !strings.Contains(err.Error(), `unknown key "baseURL"`) {
		t.Fatalf("start = %v, want an unknown key \"baseURL\" error", err)
	}
}

// Without Config.BaseURL or a resolver, the application does not start.
func TestNoOriginFailsStart(t *testing.T) {
	_, err := collagetest.TrySite(t, siteOptions(defaults, "")...)
	if !errors.Is(err, meta.ErrNoBaseURL) {
		t.Fatalf("start = %v, want ErrNoBaseURL", err)
	}
}

// Without the plugin, Set still declares, and cannot make a path absolute.
func TestSetWithoutPlugin(t *testing.T) {
	meta.Set(nil, meta.Page{Title: "x"})
	page := collage.NewPage("p").WithLayouts(layoutFragment()).WithContent(collage.NewFragment("p", "p.html").WithData(collage.Effect(
		func(_ context.Context, rc *collage.RenderContext) error {
			meta.Set(rc, meta.Page{Image: "/a.png", Canonical: "https://example.com/p"})
			return nil
		})).Build()).WithPath("en", "/").Build()
	s := collagetest.NewSite(t,
		collagetest.WithoutHome(),
		collagetest.WithTemplates(map[string]string{"p.html": `<main>p</main>`}),
		collagetest.WithRegister(page),
	)
	contains(t, s.Get("/").Body, `<meta property="og:image" content="/a.png">`, `<link rel="canonical" href="https://example.com/p">`)
}

// origins resolves two hosts, as elagoht/tenant would.
type origins struct{}

func (origins) Name() string                             { return "test/origins" }
func (origins) Version() string                          { return "0" }
func (origins) Init(context.Context, collage.Host) error { return nil }
func (origins) Shutdown(context.Context) error           { return nil }
func (origins) Origin(_ context.Context, host string) (string, bool) {
	switch host {
	case "a.test":
		return "https://a.example", true
	case "b.test":
		return "https://b.example", true
	}
	return "", false
}

// hostSite is a site with an origin resolver and no BaseURL.
func hostSite(t *testing.T) *collagetest.Site {
	t.Helper()
	return collagetest.NewSite(t, collagetest.WithoutHome(),
		collagetest.WithTemplates(map[string]string{"p.html": `<main>page</main>`}),
		collagetest.WithRegister(collage.NewPage("home").WithLayouts(layoutFragment()).
			WithContent(collage.NewFragment("home", "p.html").Build()).
			WithPath("en", "/").WithPath("tr", "/").Build()),
		collagetest.WithConfig(func(c *collage.Config) {
			c.Locale = collage.LocaleConfig{Default: "en", Supported: []string{"en", "tr"}}
		}),
		collagetest.WithPlugins(origins{}, meta.New(meta.Options{DefaultImage: "/share.png"})),
	)
}

func TestMeta_CanonicalFollowsHost(t *testing.T) {
	s := hostSite(t)
	for host, origin := range map[string]string{"a.test": "https://a.example", "b.test": "https://b.example"} {
		contains(t, s.Get("http://"+host+"/").Body,
			`<link rel="canonical" href="`+origin+`/">`,
			`<meta property="og:url" content="`+origin+`/">`,
			`<meta property="og:image" content="`+origin+`/share.png">`,
			`<link rel="alternate" hreflang="tr" href="`+origin+`/tr">`,
		)
	}
}

// A host the resolver does not know falls back to nothing: no absolute URLs, and
// the page still renders.
func TestMeta_UnknownHostOmitsURLs(t *testing.T) {
	res := hostSite(t).Get("http://c.test/").WantStatus(http.StatusOK)
	lacks(t, res.Body, `canonical`, `og:image"`)
}

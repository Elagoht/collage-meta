// Package meta is a collage plugin that writes the tags a page is shared and
// indexed by into its head: Open Graph, Twitter cards, the canonical URL, the
// meta description, and the page's translations.
//
//	app, err := collage.New(&collage.Config{
//		Plugins: []collage.Plugin{meta.New(meta.Options{
//			SiteName:     "The blog",
//			BaseURL:      "https://example.com",
//			DefaultImage: "/static/share.png",
//		})},
//	})
//
// Registered, every page gets the site's defaults and what can be worked out
// without asking it: its canonical URL, og:url, og:locale, and a
// <link rel="alternate" hreflang> for every locale it has a path in. What a page is
// about it says from its data handler, where it already has the article:
//
//	func articleData(ctx context.Context, rc *collage.RenderContext) (view, []string, error) {
//		article, err := client.Article(ctx, rc.Param("slug"))
//		// ...
//		meta.Set(rc, meta.Page{
//			Title:       article.Title,
//			Description: article.Dek,
//			Image:       article.Cover,
//			Type:        meta.Article,
//			Published:   article.PublishedAt,
//		})
//		return view{Article: article}, nil, nil
//	}
//
// Every tag is hoisted under a key of its own, the keys collage's own
// rc.HoistMeta, rc.HoistProperty, rc.HoistLink and rc.HoistAlternate use, so the
// more specific declaration wins tag by tag: the page's title over nothing, its
// image over the site's, and a fragment's own rc.HoistMeta("description", ...)
// over either.
package meta

import (
	"context"
	"errors"
	"fmt"
	"html"
	"html/template"
	"net/url"
	"strings"
	"time"

	"github.com/Elagoht/collage/pkg/collage"
)

// Name is the plugin's name, and the key its configuration is found under.
const Name = "elagoht/meta"

// The og:type values a page is usually one of.
const (
	Website = "website"
	Article = "article"
)

// The twitter:card values.
const (
	Summary           = "summary"
	SummaryLargeImage = "summary_large_image"
)

// Options configures the plugin: the site-wide defaults every page starts from.
type Options struct {
	// SiteName is og:site_name.
	SiteName string `json:"siteName"`
	// BaseURL is the site's origin, "https://example.com". A canonical URL and an
	// og:image are absolute, and the application cannot know its own host; falls
	// back to the application's Config.BaseURL when empty.
	BaseURL string `json:"baseURL"`
	// DefaultImage is the og:image of a page that names none: a path on the site,
	// made absolute with BaseURL, or an absolute URL.
	DefaultImage string `json:"defaultImage"`
	// DefaultImageAlt describes DefaultImage.
	DefaultImageAlt string `json:"defaultImageAlt"`
	// TwitterSite is the site's own account, "@example", sent as twitter:site.
	TwitterSite string `json:"twitterSite"`
	// Locales maps a collage locale to the og:locale it is written as: "en" to
	// "en_US". A locale not in it is written as it is, with "-" made "_".
	Locales map[string]string `json:"locales"`
	// NoAlternates leaves out the <link rel="alternate" hreflang> tags, for a site
	// whose layout writes its own.
	NoAlternates bool `json:"noAlternates"`
}

// Page is what a page says about itself. Every field is optional; one left empty
// leaves the site's default, or nothing, in its place.
type Page struct {
	// Title is og:title. The page's <title> is its own business: rc.HoistTitle or
	// the fragment's Title.
	Title string
	// Description is <meta name="description"> and og:description.
	Description string
	// Image is og:image: a path on the site, made absolute with BaseURL, or an
	// absolute URL. Naming one makes the Twitter card a large-image one.
	Image string
	// ImageAlt describes Image, as og:image:alt and twitter:image:alt.
	ImageAlt string
	// Type is og:type: Website, the default, or Article.
	Type string
	// Canonical replaces the canonical URL worked out from the page's own path —
	// for a page that is a copy of another. A path is made absolute with BaseURL.
	Canonical string
	// TwitterCard is twitter:card: Summary or SummaryLargeImage. Default the
	// latter when there is an image, the former when there is none.
	TwitterCard string
	// Published and Modified are article:published_time and
	// article:modified_time.
	Published time.Time
	Modified  time.Time
	// Author is article:author.
	Author string
}

// Plugin writes the tags.
type Plugin struct {
	opts Options
	host collage.Host
	site *site
}

// site is what Set needs of the plugin's configuration, handed to it through the
// render: Set is a function of the render, not of the plugin, as collage-jsonld's
// Emit is.
type site struct {
	base, scheme string
}

// siteKey is where the plugin leaves its site in a render's SharedData.
const siteKey = Name + ":site"

// The hooks the plugin means to implement; a misspelt method would otherwise be a
// hook that silently never fires.
var (
	_ collage.Plugin           = (*Plugin)(nil)
	_ collage.BeforeRenderHook = (*Plugin)(nil)
)

// New returns a plugin with opts as its starting point, which the application's
// own configuration is then decoded over.
func New(opts Options) *Plugin { return &Plugin{opts: opts} }

func (p *Plugin) Name() string                   { return Name }
func (p *Plugin) Version() string                { return "0.1.4" }
func (p *Plugin) Shutdown(context.Context) error { return nil }

// ErrNoBaseURL is returned by Init without an absolute BaseURL.
var ErrNoBaseURL = errors.New("meta: BaseURL is required: canonical URLs and images are absolute")

// Init reads the configuration and refuses a site with no origin.
func (p *Plugin) Init(_ context.Context, host collage.Host) error {
	if err := host.Config(&p.opts); err != nil {
		return err
	}
	// The plugin's own BaseURL wins; otherwise the application's Config.BaseURL,
	// which collage validated and reports without a trailing slash.
	if p.opts.BaseURL == "" {
		p.opts.BaseURL = host.BaseURL()
	}
	base, err := url.Parse(p.opts.BaseURL)
	if p.opts.BaseURL == "" || err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return fmt.Errorf("%w, got %q", ErrNoBaseURL, p.opts.BaseURL)
	}
	if base.RawQuery != "" || base.Fragment != "" {
		return fmt.Errorf("meta: BaseURL %q must be an origin, without a query or a fragment", p.opts.BaseURL)
	}
	p.host = host
	p.site = &site{base: strings.TrimSuffix(p.opts.BaseURL, "/"), scheme: base.Scheme}
	return nil
}

// OnBeforeRender declares the site's defaults and the page's own URLs. Declared
// here, before any fragment runs, they sit at depth zero, so whatever the page
// declares under the same key wins.
func (p *Plugin) OnBeforeRender(_ context.Context, ev *collage.BeforeRenderEvent) error {
	rc := ev.Context
	if rc == nil || p.site == nil {
		return nil
	}
	rc.Set(siteKey, p.site)
	o := p.opts

	if o.SiteName != "" {
		rc.HoistProperty("og:site_name", o.SiteName)
	}
	rc.HoistProperty("og:type", Website)
	if canonical := p.pageURL(rc, rc.Locale); canonical != "" {
		rc.HoistLink("canonical", canonical)
		rc.HoistProperty("og:url", canonical)
	}
	p.locales(rc)

	card := Summary
	if o.DefaultImage != "" {
		rc.HoistProperty("og:image", p.site.absolute(o.DefaultImage))
		if o.DefaultImageAlt != "" {
			rc.HoistProperty("og:image:alt", o.DefaultImageAlt)
			rc.HoistMeta("twitter:image:alt", o.DefaultImageAlt)
		}
		card = SummaryLargeImage
	}
	rc.HoistMeta("twitter:card", card)
	if o.TwitterSite != "" {
		rc.HoistMeta("twitter:site", o.TwitterSite)
	}
	return nil
}

// pageURL is the absolute URL of the page being rendered, in locale, or "" when it
// has none there — an error page reached at a URL of someone else's, a pattern the
// render's parameters do not fill.
func (p *Plugin) pageURL(rc *collage.RenderContext, locale string) string {
	if rc.Page == nil || rc.Page.Name == "" {
		return ""
	}
	path, err := p.host.URL(rc.Page.Name, locale, rc.PathParams)
	if err != nil {
		return ""
	}
	return p.site.base + path
}

// locales declares og:locale, and for a page in more than one locale the others:
// og:locale:alternate, and <link rel="alternate" hreflang> for each and for
// x-default, which is what tells a search engine the page's translations.
func (p *Plugin) locales(rc *collage.RenderContext) {
	defaultLocale, supported := p.host.Locales()
	if rc.Locale != "" {
		rc.HoistProperty("og:locale", p.ogLocale(rc.Locale))
	}
	type translation struct{ locale, url string }
	var in []translation
	for _, locale := range supported {
		if u := p.pageURL(rc, locale); u != "" {
			in = append(in, translation{locale, u})
		}
	}
	if len(in) < 2 {
		return
	}
	for _, t := range in {
		if t.locale != rc.Locale {
			og := p.ogLocale(t.locale)
			rc.Hoist("head", "property:og:locale:alternate:"+og, template.HTML( // assembled here from escaped values
				`<meta property="og:locale:alternate" content="`+html.EscapeString(og)+`">`))
		}
		if !p.opts.NoAlternates {
			rc.HoistAlternate(t.locale, t.url)
		}
	}
	if !p.opts.NoAlternates {
		for _, t := range in {
			if t.locale == defaultLocale {
				rc.HoistAlternate("x-default", t.url)
			}
		}
	}
}

func (p *Plugin) ogLocale(locale string) string {
	if og, ok := p.opts.Locales[locale]; ok && og != "" {
		return og
	}
	return strings.ReplaceAll(locale, "-", "_")
}

// Set declares what the page is about. Call it from a data handler, on the
// handler's own goroutine, as any hoist is: a fragment deeper in the tree wins over
// one above it, and every fragment over the site's defaults.
//
// Outside a render the plugin set up — a test with no plugin — a relative Image or
// Canonical cannot be made absolute and is written as it is.
func Set(rc *collage.RenderContext, page Page) {
	if rc == nil {
		return
	}
	s, _ := collage.Get[*site](rc, siteKey)
	if page.Title != "" {
		rc.HoistProperty("og:title", page.Title)
	}
	if page.Description != "" {
		rc.HoistMeta("description", page.Description)
		rc.HoistProperty("og:description", page.Description)
	}
	if page.Type != "" {
		rc.HoistProperty("og:type", page.Type)
	}
	if page.Canonical != "" {
		canonical := s.absolute(page.Canonical)
		rc.HoistLink("canonical", canonical)
		rc.HoistProperty("og:url", canonical)
	}
	card := page.TwitterCard
	if page.Image != "" {
		rc.HoistProperty("og:image", s.absolute(page.Image))
		if card == "" {
			card = SummaryLargeImage
		}
	}
	if page.ImageAlt != "" {
		rc.HoistProperty("og:image:alt", page.ImageAlt)
		rc.HoistMeta("twitter:image:alt", page.ImageAlt)
	}
	if card != "" {
		rc.HoistMeta("twitter:card", card)
	}
	if !page.Published.IsZero() {
		rc.HoistProperty("article:published_time", page.Published.Format(time.RFC3339))
	}
	if !page.Modified.IsZero() {
		rc.HoistProperty("article:modified_time", page.Modified.Format(time.RFC3339))
	}
	if page.Author != "" {
		rc.HoistProperty("article:author", page.Author)
	}
}

// absolute makes a link on the site absolute: a path is joined to the base, a
// protocol-relative URL given the base's scheme, and an absolute URL kept. Without a site —
// the plugin is not registered — the link is left as it is.
func (s *site) absolute(link string) string {
	if u, err := url.Parse(link); err == nil && u.IsAbs() {
		return link
	}
	if s == nil {
		return link
	}
	if strings.HasPrefix(link, "//") {
		return s.scheme + ":" + link
	}
	if !strings.HasPrefix(link, "/") {
		link = "/" + link
	}
	return s.base + link
}

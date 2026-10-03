# elagoht/meta

A collage plugin that writes the tags a page is shared and indexed by into its
head: Open Graph, Twitter cards, the canonical URL, the meta description, and the
page's translations — from site-wide defaults, from what can be worked out, and
from what each page says about itself.

```go
app, err := collage.New(&collage.Config{
	Plugins: []collage.Plugin{meta.New(meta.Options{
		SiteName:     "The blog",
		BaseURL:      "https://example.com",
		DefaultImage: "/static/share.png",
		TwitterSite:  "@example",
	})},
})
```

Requires collage v0.23.0 or later. The tags are hoisted, so the layout needs the
marker:

```html
<head>
  {{hoist "head"}}
</head>
```

## What every page gets

Registering the plugin is enough for these, on every page:

```html
<meta property="og:site_name" content="The blog">
<meta property="og:type" content="website">
<link rel="canonical" href="https://example.com/blog/hello">
<meta property="og:url" content="https://example.com/blog/hello">
<meta property="og:locale" content="en_US">
<meta property="og:locale:alternate" content="tr">
<link rel="alternate" hreflang="en" href="https://example.com/blog/hello">
<link rel="alternate" hreflang="tr" href="https://example.com/tr/yazi/hello">
<link rel="alternate" hreflang="x-default" href="https://example.com/blog/hello">
<meta property="og:image" content="https://example.com/static/share.png">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:site" content="@example">
```

- **The canonical URL** is `BaseURL` and the page's own path in the locale it is
  rendered in, built by name as `{{pageURL}}` builds it — so the query string a
  reader arrived with, a tracking parameter included, is never part of it.
- **The translations** are every supported locale the page has a path in. A page
  in one locale gets none; `x-default` is the default locale's URL.
- **`og:locale`** is the render's locale, with `-` made `_`; `Locales` maps a
  locale to the form Open Graph wants, `"en": "en_US"`.
- **The image** is `DefaultImage`, made absolute. With one the card is
  `summary_large_image`, without one `summary`.

## What a page says

What a page is about only the page knows. It says so from its data handler, where
it already has the article:

```go
func articleData(ctx context.Context, rc *collage.RenderContext) (view, []string, error) {
	article, err := client.Article(ctx, rc.Param("slug"))
	if err != nil {
		return view{}, nil, err
	}
	meta.Set(rc, meta.Page{
		Title:       article.Title,
		Description: article.Dek,
		Image:       article.Cover, // "/uploads/cover.jpg" is made absolute
		ImageAlt:    article.CoverAlt,
		Type:        meta.Article,
		Published:   article.PublishedAt,
		Modified:    article.UpdatedAt,
		Author:      article.Author,
	})
	return view{Article: article}, nil, nil
}
```

| Field | Writes |
| --- | --- |
| `Title` | `og:title` |
| `Description` | `<meta name="description">`, `og:description` |
| `Image`, `ImageAlt` | `og:image`, `og:image:alt`, `twitter:image:alt`; an image makes the card a large one |
| `Type` | `og:type`: `meta.Website` or `meta.Article` |
| `Canonical` | `<link rel="canonical">` and `og:url`, for a page that is a copy of another |
| `TwitterCard` | `twitter:card`: `meta.Summary` or `meta.SummaryLargeImage` |
| `Published`, `Modified` | `article:published_time`, `article:modified_time`, RFC 3339 |
| `Author` | `article:author` |

An empty field leaves the default, or nothing, in its place. Every value is
escaped. A relative `Image` or `Canonical` — `/uploads/a.jpg`, `uploads/a.jpg` —
is joined to `BaseURL`; an absolute URL is kept.

The page's `<title>` is not among them: it is `rc.HoistTitle` or the fragment's
`WithTitle`, as it is without the plugin.

## Which declaration wins

Each tag is hoisted under a key of its own — the key collage's own
`rc.HoistMeta`, `rc.HoistProperty`, `rc.HoistLink` and `rc.HoistAlternate` use —
so the more specific declaration wins tag by tag. The site's defaults are declared
before any fragment runs, at depth zero; a page's `meta.Set` replaces the ones it
names and leaves the rest; a fragment deeper in the tree replaces its parent's.
A fragment calling `rc.HoistMeta("description", …)` itself replaces the plugin's
description too, and appears once.

## Configuration

```json
{
  "elagoht/meta": {
    "siteName": "The blog",
    "baseURL": "https://example.com",
    "defaultImage": "/static/share.png",
    "defaultImageAlt": "The blog's logo on blue",
    "twitterSite": "@example",
    "locales": { "en": "en_US", "tr": "tr_TR" },
    "noAlternates": false
  }
}
```

`baseURL` is an `http` or `https` origin with no query. Leave it empty and URLs
follow the origin collage resolves for the request's host (collage v0.42.0): the
application's `Config.BaseURL`, or per host the origin a plugin implementing
`collage.OriginResolver` (such as `elagoht/tenant`) gives, so one site serves each
host its own canonical, `og:url`, `og:image` and `hreflang` links. A host with no
origin gets no absolute URLs. The application does not start when no source of an
origin exists. `noAlternates` leaves the `hreflang` links out, for a
layout that writes its own; `og:locale:alternate` is still written.

## Limitations

- A page reached through a pattern and parameters gets its canonical URL from the
  render's parameters. Two spellings of one parameter — `/blog/Hello` and
  `/blog/hello` — are two canonical URLs, because the plugin cannot know they are
  one page.
- The translations are the locales the page has a *path* in. A page registered in
  Turkish whose Turkish content does not exist for this parameter still gets a
  Turkish alternate; say otherwise with `NoAlternates` and your own
  `rc.HoistAlternate` calls.
- An error page rendered at someone else's URL has no path of its own, and gets no
  canonical URL.
- Outside a render the plugin set up — a test without it — `meta.Set` still
  declares, but cannot make a relative path absolute.

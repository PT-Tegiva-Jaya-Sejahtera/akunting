// Package static memuat aset frontend (CSS, HTMX) yang di-embed ke dalam
// binary lewat go:embed, sesuai prinsip single-binary zero-dependency.
package static

import "embed"

//go:embed css/app.css js/*.js fonts/*.woff2
var Files embed.FS

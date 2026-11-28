// Package assets is the web app's built files and the site's styles, fonts and images,
// served under /assets/.
package assets

import "embed"

// FS is the whole directory, so Go builds before web/ has written app/ and js/. It
// holds this file too, which the server refuses to serve.
//
//go:embed *
var FS embed.FS

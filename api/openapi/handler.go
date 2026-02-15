// Package openapi provides the generated OpenAPI 3.1 spec and Swagger UI routes.
package openapi

import (
	"embed"
	"net/http"

	"github.com/labstack/echo/v4"
)

//go:embed swagger.json swagger.yaml
var specFS embed.FS

const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Server Price Tracker API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: "/swagger/swagger.json",
      dom_id: "#swagger-ui",
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
      layout: "BaseLayout",
    });
  </script>
</body>
</html>`

// RegisterRoutes adds Swagger UI and spec endpoints to the Echo instance.
func RegisterRoutes(e *echo.Echo) {
	e.GET("/swagger/swagger.json", serveSpec("swagger.json", "application/json"))
	e.GET("/swagger/swagger.yaml", serveSpec("swagger.yaml", "text/yaml"))
	e.GET("/swagger/index.html", serveUI)
	e.GET("/swagger", redirectToUI)
	e.GET("/swagger/", redirectToUI)
}

func serveSpec(filename, contentType string) echo.HandlerFunc {
	return func(c echo.Context) error {
		data, err := specFS.ReadFile(filename)
		if err != nil {
			return c.String(http.StatusInternalServerError, "spec not found")
		}
		return c.Blob(http.StatusOK, contentType, data)
	}
}

func serveUI(c echo.Context) error {
	return c.HTML(http.StatusOK, swaggerUIHTML)
}

func redirectToUI(c echo.Context) error {
	return c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
}

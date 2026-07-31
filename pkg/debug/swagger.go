package debug

import "encoding/json"

// patchSwagger подставляет в спецификацию host/schemes/title, чтобы
// Swagger UI (живущий на debug-порте) слал запросы Try it out на
// публичный HTTP-порт (в docker-compose — localhost:80).
func patchSwagger(raw []byte, targetHost, title string) []byte {
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		return raw
	}

	if targetHost != "" {
		spec["host"] = targetHost
	}
	spec["schemes"] = []string{"http"}

	if title != "" {
		info, ok := spec["info"].(map[string]any)
		if !ok {
			info = map[string]any{}
		}
		info["title"] = title
		spec["info"] = info
	}

	patched, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return raw
	}
	return patched
}

const swaggerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>API — Swagger UI</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css"/>
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
  window.ui = SwaggerUIBundle({
    url: "/swagger.json",
    dom_id: "#swagger-ui",
    deepLinking: true,
    tryItOutEnabled: true
  });
</script>
</body>
</html>
`

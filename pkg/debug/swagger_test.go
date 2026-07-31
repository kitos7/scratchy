package debug

import (
	"encoding/json"
	"testing"
)

func unmarshal(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("результат не JSON (%v):\n%s", err, raw)
	}
	return spec
}

func TestPatchSwagger(t *testing.T) {
	raw := []byte(`{"swagger":"2.0","host":"старый:1","info":{"title":"старый","version":"1"},"paths":{}}`)

	spec := unmarshal(t, patchSwagger(raw, "localhost:8080", "billing"))

	if spec["host"] != "localhost:8080" {
		t.Errorf("host = %v, want localhost:8080", spec["host"])
	}
	if spec["info"].(map[string]any)["title"] != "billing" {
		t.Errorf("title не подменён: %v", spec["info"])
	}
	// Остальное в info трогать нельзя.
	if spec["info"].(map[string]any)["version"] != "1" {
		t.Errorf("version потерян: %v", spec["info"])
	}
	if spec["swagger"] != "2.0" {
		t.Errorf("swagger потерян: %v", spec)
	}

	schemes, ok := spec["schemes"].([]any)
	if !ok || len(schemes) != 1 || schemes[0] != "http" {
		t.Errorf("schemes = %v, want [http]", spec["schemes"])
	}
}

// Пустой targetHost — «оставь как есть»: иначе Swagger UI начнёт слать
// запросы на пустой хост.
func TestPatchSwagger_EmptyTargetHostKeepsHost(t *testing.T) {
	raw := []byte(`{"host":"было:1","info":{"title":"t"}}`)

	spec := unmarshal(t, patchSwagger(raw, "", "billing"))

	if spec["host"] != "было:1" {
		t.Errorf("host = %v, want 'было:1'", spec["host"])
	}
}

func TestPatchSwagger_EmptyTitleKeepsTitle(t *testing.T) {
	raw := []byte(`{"info":{"title":"было"}}`)

	spec := unmarshal(t, patchSwagger(raw, "h", ""))

	if spec["info"].(map[string]any)["title"] != "было" {
		t.Errorf("title = %v, want 'было'", spec["info"])
	}
}

// Спека без info: title всё равно должен появиться, а не потеряться.
func TestPatchSwagger_MissingInfo(t *testing.T) {
	spec := unmarshal(t, patchSwagger([]byte(`{"swagger":"2.0"}`), "h", "billing"))

	info, ok := spec["info"].(map[string]any)
	if !ok {
		t.Fatalf("info не создан: %v", spec)
	}
	if info["title"] != "billing" {
		t.Errorf("title = %v, want billing", info)
	}
}

// Битую спеку возвращаем как есть — Swagger UI хотя бы покажет свою ошибку,
// вместо того чтобы получить пустой ответ.
func TestPatchSwagger_InvalidJSONReturnedAsIs(t *testing.T) {
	raw := []byte(`{это не json`)

	got := patchSwagger(raw, "h", "t")

	if string(got) != string(raw) {
		t.Errorf("битая спека изменена:\n%s", got)
	}
}

// info не объект (кривая генерация) — не паникуем и не теряем title.
func TestPatchSwagger_InfoWrongType(t *testing.T) {
	spec := unmarshal(t, patchSwagger([]byte(`{"info":"строка"}`), "h", "billing"))

	info, ok := spec["info"].(map[string]any)
	if !ok || info["title"] != "billing" {
		t.Errorf("info не перезаписан объектом: %v", spec["info"])
	}
}

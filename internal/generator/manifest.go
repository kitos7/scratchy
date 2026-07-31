package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ManifestName — имя манифеста в корне сгенерированного проекта.
const ManifestName = ".scratch.yaml"

// currentSchema — версия формата манифеста, которую понимает этот бинарник.
// Поднимается при несовместимом изменении структуры Manifest.
const currentSchema = 1

// Manifest фиксирует версию шаблонов и параметры генерации —
// на нём строится `scratch update`.
type Manifest struct {
	Schema         int               `yaml:"schema"`
	ScratchVersion string            `yaml:"scratch_version"`
	GeneratedAt    time.Time         `yaml:"generated_at"`
	Params         ManifestParams    `yaml:"params"`
	Managed        map[string]string `yaml:"managed"`
}

// ManifestParams — сохранённые параметры генерации.
type ManifestParams struct {
	Module      string `yaml:"module"`
	AppName     string `yaml:"app_name"`
	Package     string `yaml:"package"`
	ServiceName string `yaml:"service_name"`
	GoVersion   string `yaml:"go_version"`
	LibReplace  string `yaml:"lib_replace,omitempty"`
}

// NewManifest создаёт манифест из параметров генерации.
func NewManifest(p Params) *Manifest {
	return &Manifest{
		Schema:         currentSchema,
		ScratchVersion: p.ScratchVersion,
		Params: ManifestParams{
			Module:      p.Module,
			AppName:     p.AppName,
			Package:     p.Package,
			ServiceName: p.ServiceName,
			GoVersion:   p.GoVersion,
			LibReplace:  p.LibReplace,
		},
		Managed: map[string]string{},
	}
}

// ToParams восстанавливает параметры рендера из манифеста.
func (m *Manifest) ToParams(scratchVersion string) Params {
	libIsSemver := semverRe.MatchString(scratchVersion)
	libVersion := placeholderVersion
	if libIsSemver {
		libVersion = scratchVersion
	}
	return Params{
		Module:             m.Params.Module,
		AppName:            m.Params.AppName,
		Package:            m.Params.Package,
		ServiceName:        m.Params.ServiceName,
		ServerPkg:          ServerPackage(m.Params.ServiceName + "Service"),
		GoVersion:          m.Params.GoVersion,
		ScratchVersion:     scratchVersion,
		ScratchModule:      ScratchModule,
		LibVersion:         libVersion,
		LibIsSemver:        libIsSemver,
		ScratchInstallable: installable(libIsSemver, m.Params.LibReplace),
		LibReplace:         m.Params.LibReplace,
	}
}

// Save пишет манифест в dir.
func (m *Manifest) Save(dir string) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	header := []byte("# Манифест scratch: версия шаблонов и параметры генерации.\n# Файл нужен команде `scratch update` — не удаляй и не правь руками.\n")
	return os.WriteFile(filepath.Join(dir, ManifestName), append(header, data...), 0o644)
}

// LoadManifest читает манифест из dir.
func LoadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("проект не похож на сгенерированный scratch (нет %s): %w", ManifestName, err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ManifestName, err)
	}
	// Манифест писал другой бинарник, и формат мог измениться несовместимо:
	// лучше сказать это прямо, чем молча отработать по чужой структуре.
	if m.Schema > currentSchema {
		return nil, fmt.Errorf("%s имеет schema %d, а этот scratch понимает %d — обнови scratch",
			ManifestName, m.Schema, currentSchema)
	}
	if m.Managed == nil {
		m.Managed = map[string]string{}
	}
	return &m, nil
}

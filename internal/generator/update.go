package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/mod/modfile"
)

// FileStatus — результат обновления одного managed-файла.
type FileStatus string

// Статусы файлов при обновлении.
const (
	StatusCreated   FileStatus = "created"   // файла не было — создан
	StatusUpdated   FileStatus = "updated"   // не был изменён руками — перезаписан
	StatusUnchanged FileStatus = "unchanged" // новая версия совпадает с текущей
	StatusConflict  FileStatus = "conflict"  // менялся руками — новая версия рядом (*.scratch-new)
)

// UpdateReport — итог scratch update.
type UpdateReport struct {
	FromVersion string
	ToVersion   string
	Files       map[string]FileStatus
	LibBumped   bool
	Warnings    []string
}

// Update обновляет managed-файлы проекта до шаблонов текущей версии scratch
// и поднимает версию платформенной либы в go.mod.
func Update(dir, scratchVersion string) (*UpdateReport, error) {
	manifest, err := LoadManifest(dir)
	if err != nil {
		return nil, err
	}

	params := manifest.ToParams(scratchVersion)
	rendered, err := Render(params)
	if err != nil {
		return nil, err
	}

	report := &UpdateReport{
		FromVersion: manifest.ScratchVersion,
		ToVersion:   scratchVersion,
		Files:       map[string]FileStatus{},
	}

	for rel, newContent := range rendered {
		if !managedFiles[rel] {
			continue
		}
		full := filepath.Join(dir, filepath.FromSlash(rel))
		newHash := hashBytes(newContent)

		current, err := os.ReadFile(full)
		switch {
		case os.IsNotExist(err):
			if werr := writeFile(full, newContent); werr != nil {
				return nil, werr
			}
			manifest.Managed[rel] = newHash
			report.Files[rel] = StatusCreated

		case err != nil:
			return nil, fmt.Errorf("read %s: %w", rel, err)

		default:
			currentHash := hashBytes(current)
			switch currentHash {
			case newHash:
				manifest.Managed[rel] = newHash
				report.Files[rel] = StatusUnchanged
			case manifest.Managed[rel]:
				// Файл не менялся руками — безопасно перезаписываем.
				if werr := writeFile(full, newContent); werr != nil {
					return nil, werr
				}
				manifest.Managed[rel] = newHash
				report.Files[rel] = StatusUpdated
			default:
				// Файл менялся руками — не затираем, кладём новую версию рядом.
				if werr := writeFile(full+".scratch-new", newContent); werr != nil {
					return nil, werr
				}
				report.Files[rel] = StatusConflict
			}
		}
	}

	bumped, warn := bumpLibVersion(dir, scratchVersion)
	report.LibBumped = bumped
	if warn != "" {
		report.Warnings = append(report.Warnings, warn)
	}

	manifest.ScratchVersion = scratchVersion
	manifest.GeneratedAt = time.Now().UTC()
	if err := manifest.Save(dir); err != nil {
		return nil, err
	}

	return report, nil
}

// bumpLibVersion поднимает require платформенной либы в go.mod.
func bumpLibVersion(dir, scratchVersion string) (bool, string) {
	if !semverRe.MatchString(scratchVersion) {
		return false, fmt.Sprintf("scratch собран без semver-версии (%s) — версия %s в go.mod не изменена", scratchVersion, ScratchModule)
	}

	gomodPath := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(gomodPath)
	if err != nil {
		return false, fmt.Sprintf("go.mod не прочитан: %v", err)
	}

	mf, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return false, fmt.Sprintf("go.mod не разобран: %v", err)
	}

	if err := mf.AddRequire(ScratchModule, scratchVersion); err != nil {
		return false, fmt.Sprintf("не удалось обновить require: %v", err)
	}
	mf.Cleanup()

	out, err := mf.Format()
	if err != nil {
		return false, fmt.Sprintf("go.mod не сформатирован: %v", err)
	}
	if err := os.WriteFile(gomodPath, out, 0o644); err != nil {
		return false, fmt.Sprintf("go.mod не записан: %v", err)
	}
	return true, ""
}

func writeFile(full string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", full, err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", full, err)
	}
	return nil
}

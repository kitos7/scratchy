package generator

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"golang.org/x/mod/module"
)

// ScratchModule — module path платформенной либы, которую импортируют
// сгенерированные проекты.
const ScratchModule = "github.com/kitos7/scratchy"

// placeholderVersion используется в require, пока scratch не имеет
// semver-релиза (сборка dev) — тогда обязателен replace.
const placeholderVersion = "v0.0.0-00010101000000-000000000000"

var (
	semverRe  = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	appNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// Params — параметры рендера шаблонов.
type Params struct {
	// Module — module path генерируемого проекта (github.com/acme/demo).
	Module string
	// AppName — имя приложения: директория, бинарник, cmd/<AppName>.
	AppName string
	// Package — go/proto-безопасное короткое имя (demo).
	Package string
	// ServiceName — PascalCase-имя для proto-сервиса (Demo → DemoService).
	ServiceName string
	// ServerPkg — пакет транспорта для proto-сервиса (DemoService → demoservice).
	ServerPkg string
	// GoVersion — версия Go для go.mod и Dockerfile (например, 1.24).
	GoVersion string
	// ScratchVersion — версия бинарника scratch (semver или dev).
	ScratchVersion string
	// ScratchModule — module path платформенной либы.
	ScratchModule string
	// LibVersion — версия либы для require в go.mod.
	LibVersion string
	// LibIsSemver — у scratch есть semver-релиз, значит версию можно
	// указать в require без плейсхолдера.
	LibIsSemver bool
	// ScratchInstallable — бинарник scratch резолвится из сети:
	// `go install <module>/cmd/scratch@<version>` сработает. Это не то же,
	// что LibIsSemver: тег может быть, а модуль — лежать только локально
	// (тогда стоит replace, и scratch собирается из этой копии).
	ScratchInstallable bool
	// LibReplace — путь для replace-директивы (локальная разработка).
	LibReplace string
}

// NewParams валидирует вход и выводит производные параметры.
func NewParams(modulePath, name, libReplace, goVersion, scratchVersion string) (Params, error) {
	if err := module.CheckPath(modulePath); err != nil {
		return Params{}, fmt.Errorf("некорректный module path %q: %w", modulePath, err)
	}

	if name == "" {
		name = path.Base(modulePath)
	}
	name = strings.ToLower(name)
	if !appNameRe.MatchString(name) {
		return Params{}, fmt.Errorf("некорректное имя приложения %q: допустимы [a-z0-9-], первая буква — [a-z]", name)
	}

	pkg := strings.ReplaceAll(name, "-", "")
	libVersion := placeholderVersion
	libIsSemver := semverRe.MatchString(scratchVersion)
	if libIsSemver {
		libVersion = scratchVersion
	}

	if goVersion == "" {
		goVersion = "1.24"
	}

	svcName := serviceName(name)

	return Params{
		Module:             modulePath,
		AppName:            name,
		Package:            pkg,
		ServiceName:        svcName,
		ServerPkg:          ServerPackage(svcName + "Service"),
		GoVersion:          goVersion,
		ScratchVersion:     scratchVersion,
		ScratchModule:      ScratchModule,
		LibVersion:         libVersion,
		LibIsSemver:        libIsSemver,
		ScratchInstallable: installable(libIsSemver, libReplace),
		LibReplace:         libReplace,
	}, nil
}

// installable отвечает на вопрос «сработает ли go install либы из сети»:
// нужен semver-тег и отсутствие replace на локальную копию.
func installable(libIsSemver bool, libReplace string) bool {
	return libIsSemver && libReplace == ""
}

// ServerPackage превращает имя proto-сервиса в имя Go-пакета транспорта:
// DemoService → demoservice, Demo_Service_V2 → demoservicev2.
func ServerPackage(protoService string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(protoService) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// serviceName превращает имя приложения в имя proto-сервиса:
// my-service → MyService, e2edemo → E2Edemo.
//
// Имя обязано быть неподвижной точкой GoCamelCase: то же имя объявляется
// в proto и подставляется в Go-код шаблонов, а protoc-gen-go мангли́т
// proto-имена по своим правилам. Разойдись они — сгенерированный проект
// не соберётся (ссылки на Unimplemented<Name>Server и Register<Name>Server).
func serviceName(name string) string {
	return GoCamelCase(strings.ReplaceAll(name, "-", "_"))
}

// GoCamelCase повторяет правила protoc-gen-go (protobuf-go strs.GoCamelCase):
// по ним имена из proto превращаются в идентификаторы сгенерированного
// Go-кода. Пакет с оригиналом внутренний, поэтому алгоритм воспроизведён:
// "_" перед буквой поднимает её регистр, "." становится "_", а буква сразу
// после цифры тоже поднимается — из-за последнего "e2edemo" даёт "E2Edemo".
func GoCamelCase(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '.' && i+1 < len(s) && isASCIILower(s[i+1]):
			// Точка перед строчной буквой съедается: буква станет заглавной.
		case c == '.':
			b = append(b, '_')
		case c == '_' && (i == 0 || s[i-1] == '.'):
			// Идентификатор не может начинаться с подчёркивания.
			b = append(b, 'X')
		case c == '_' && i+1 < len(s) && isASCIILower(s[i+1]):
			// Подчёркивание съедается: следующая буква станет заглавной.
		case isASCIIDigit(c):
			b = append(b, c)
		default:
			// Начало слова: поднимаем регистр и забираем строчный хвост.
			if isASCIILower(c) {
				c -= 'a' - 'A'
			}
			b = append(b, c)
			for ; i+1 < len(s) && isASCIILower(s[i+1]); i++ {
				b = append(b, s[i+1])
			}
		}
	}
	return string(b)
}

func isASCIILower(c byte) bool { return 'a' <= c && c <= 'z' }
func isASCIIDigit(c byte) bool { return '0' <= c && c <= '9' }

package build

import (
	"fmt"
	"os"
	"strings"
)

func ReadConfig(path string) (Config, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg := defaultConfig()
	parser := newConfigParser(path, string(bytes), &cfg)

	if err := parser.parse(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func defaultConfig() Config {
	return Config{
		Version: "0.1.0",
		Kind:    KindLibrary,

		Compiler:     "",
		CompilerPath: "",
		CompilerArgs: nil,
		CFlags:       nil,
		LinkFlags:    nil,
		IncludeDirs:  nil,
		LibraryDirs:  nil,
		Libraries:    nil,
		Defines:      nil,
		Target:       "",
		Standard:     "c11",
		Linkage:      "static",

		AutoInitializeVariables:        true,
		AllowUninitializedVariables:    false,
		AllowPartialInitializedStructs: false,
		AllowPartialInitializedArrays:  true,
		AllowPartialSwitches:           false,

		IntegerOverflow: "trap",
		BoundsChecking:  "trap",

		FailBadStyle:          false,
		AllowUnusedVariables:  true,
		AllowUnusedParameters: true,
		AllowRunDirectives:    true,
	}
}

type configParser struct {
	path    string
	text    string
	lines   []string
	cfg     *Config
	index   int
	section string
}

func newConfigParser(path string, text string, cfg *Config) *configParser {
	return &configParser{
		path:  path,
		text:  text,
		lines: strings.Split(text, "\n"),
		cfg:   cfg,
	}
}

func (p *configParser) parse() error {
	for p.index < len(p.lines) {
		line := p.cleanLine(p.lines[p.index])
		p.index++

		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			p.section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			if p.section == "" {
				return p.err("section name cannot be empty")
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return p.err("expected key = value")
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if p.section != "" {
			key = p.section + "." + key
		}

		if key == "dependencies" || key == "package.dependencies" {
			deps, err := p.parseDependencies(value)
			if err != nil {
				return err
			}

			p.cfg.Dependencies = deps
			continue
		}

		if err := p.assign(key, value); err != nil {
			return err
		}
	}

	return nil
}

func (p *configParser) parseDependencies(value string) ([]Dependency, error) {
	var content string

	if strings.HasPrefix(value, "[") && strings.Contains(value, "]") {
		content = value
	} else if value == "[" {
		var b strings.Builder
		b.WriteString(value)

		for p.index < len(p.lines) {
			line := p.cleanLine(p.lines[p.index])
			p.index++

			b.WriteString(line)

			if strings.Contains(line, "]") {
				break
			}
		}

		content = b.String()
	} else {
		return nil, p.err("dependencies must be an array")
	}

	content = strings.TrimSpace(content)

	if !strings.HasPrefix(content, "[") || !strings.HasSuffix(content, "]") {
		return nil, p.err("dependencies array is not closed")
	}

	content = strings.TrimPrefix(content, "[")
	content = strings.TrimSuffix(content, "]")
	content = strings.TrimSpace(content)

	if content == "" {
		return nil, nil
	}

	var deps []Dependency

	entries := splitTopLevelObjects(content)

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)

		if entry == "" {
			continue
		}

		if strings.HasPrefix(entry, `"`) {
			name, err := parseString(entry)
			if err != nil {
				return nil, err
			}

			deps = append(deps, Dependency{Name: name})
			continue
		}

		if !strings.HasPrefix(entry, "{") || !strings.HasSuffix(entry, "}") {
			return nil, p.err("dependency must be string or { name = ..., version = ... }")
		}

		entry = strings.TrimPrefix(entry, "{")
		entry = strings.TrimSuffix(entry, "}")

		dep := Dependency{}
		fields := splitComma(entry)

		for _, field := range fields {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				return nil, p.err("dependency field must be key = value")
			}

			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)

			s, err := parseString(v)
			if err != nil {
				return nil, err
			}

			switch k {
			case "name":
				dep.Name = s
			case "version":
				dep.Version = s
			default:
				return nil, p.err(fmt.Sprintf("unknown dependency field %q", k))
			}
		}

		if dep.Name == "" {
			return nil, p.err("dependency name is required")
		}

		deps = append(deps, dep)
	}

	return deps, nil
}

func (p *configParser) assign(key string, value string) error {
	switch key {
	case "name", "package.name":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		p.cfg.Name = s

	case "version", "package.version":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		p.cfg.Version = s

	case "kind", "package.kind":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		p.cfg.Kind = Kind(s)

	case "compiler", "build.compiler":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		p.cfg.Compiler = s

	case "compiler_path", "build.compiler_path":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		p.cfg.CompilerPath = s

	case "compiler_args", "build.compiler_args":
		values, err := parseStringArray(value)
		if err != nil {
			return err
		}
		p.cfg.CompilerArgs = values

	case "c_flags", "build.c_flags":
		values, err := parseStringArray(value)
		if err != nil {
			return err
		}
		p.cfg.CFlags = values

	case "link_flags", "build.link_flags":
		values, err := parseStringArray(value)
		if err != nil {
			return err
		}
		p.cfg.LinkFlags = values

	case "include_dirs", "build.include_dirs":
		values, err := parseStringArray(value)
		if err != nil {
			return err
		}
		p.cfg.IncludeDirs = values

	case "library_dirs", "build.library_dirs":
		values, err := parseStringArray(value)
		if err != nil {
			return err
		}
		p.cfg.LibraryDirs = values

	case "libraries", "build.libraries":
		values, err := parseStringArray(value)
		if err != nil {
			return err
		}
		p.cfg.Libraries = values

	case "defines", "build.defines":
		values, err := parseStringArray(value)
		if err != nil {
			return err
		}
		p.cfg.Defines = values

	case "target", "build.target":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		p.cfg.Target = s

	case "standard", "build.standard":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		p.cfg.Standard = s

	case "linkage", "build.linkage":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		p.cfg.Linkage = s

	case "auto_initialize_variables", "checks.auto_initialize_variables":
		v, err := parseBool(value)
		if err != nil {
			return err
		}
		p.cfg.AutoInitializeVariables = v

	case "allow_uninitialized_variables", "checks.allow_uninitialized_variables":
		v, err := parseBool(value)
		if err != nil {
			return err
		}
		p.cfg.AllowUninitializedVariables = v

	case "allow_partial_initialized_structs", "checks.allow_partial_initialized_structs":
		v, err := parseBool(value)
		if err != nil {
			return err
		}
		p.cfg.AllowPartialInitializedStructs = v

	case "allow_partial_initialized_arrays", "checks.allow_partial_initialized_arrays":
		v, err := parseBool(value)
		if err != nil {
			return err
		}
		p.cfg.AllowPartialInitializedArrays = v

	case "allow_partial_switches", "checks.allow_partial_switches":
		v, err := parseBool(value)
		if err != nil {
			return err
		}
		p.cfg.AllowPartialSwitches = v

	case "integer_overflow", "checks.integer_overflow":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		p.cfg.IntegerOverflow = s

	case "bounds_checking", "checks.bounds_checking":
		s, err := parseString(value)
		if err != nil {
			return err
		}
		p.cfg.BoundsChecking = s

	case "fail_bad_style", "checks.fail_bad_style":
		v, err := parseBool(value)
		if err != nil {
			return err
		}
		p.cfg.FailBadStyle = v

	case "allow_unused_variables", "checks.allow_unused_variables":
		v, err := parseBool(value)
		if err != nil {
			return err
		}
		p.cfg.AllowUnusedVariables = v

	case "allow_unused_parameters", "checks.allow_unused_parameters":
		v, err := parseBool(value)
		if err != nil {
			return err
		}
		p.cfg.AllowUnusedParameters = v

	case "allow_run_directives", "checks.allow_run_directives":
		v, err := parseBool(value)
		if err != nil {
			return err
		}
		p.cfg.AllowRunDirectives = v

	default:
		return p.err(fmt.Sprintf("unknown seal.toml key %q", key))
	}

	return nil
}

func (p *configParser) cleanLine(line string) string {
	line = strings.TrimSpace(line)

	if i := strings.Index(line, "#"); i >= 0 {
		line = line[:i]
	}

	return strings.TrimSpace(line)
}

func (p *configParser) err(message string) error {
	return fmt.Errorf("%s:%d: %s", p.path, p.index, message)
}

func parseString(value string) (string, error) {
	value = strings.TrimSpace(value)

	if !strings.HasPrefix(value, `"`) || !strings.HasSuffix(value, `"`) {
		return "", fmt.Errorf("expected string, got %q", value)
	}

	return strings.Trim(value, `"`), nil
}

func parseStringArray(value string) ([]string, error) {
	value = strings.TrimSpace(value)

	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, fmt.Errorf("expected string array, got %q", value)
	}

	content := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if content == "" {
		return nil, nil
	}

	parts := splitComma(content)
	values := make([]string, 0, len(parts))

	for _, part := range parts {
		s, err := parseString(part)
		if err != nil {
			return nil, err
		}

		values = append(values, s)
	}

	return values, nil
}

func parseBool(value string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("expected bool, got %q", value)
	}
}

func splitTopLevelObjects(input string) []string {
	var result []string
	depth := 0
	start := 0

	for i, ch := range input {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(input[start:i])
				if part != "" {
					result = append(result, part)
				}
				start = i + 1
			}
		}
	}

	last := strings.TrimSpace(input[start:])
	if last != "" {
		result = append(result, last)
	}

	return result
}

func splitComma(input string) []string {
	var result []string
	start := 0

	for i, ch := range input {
		if ch == ',' {
			part := strings.TrimSpace(input[start:i])
			if part != "" {
				result = append(result, part)
			}
			start = i + 1
		}
	}

	last := strings.TrimSpace(input[start:])
	if last != "" {
		result = append(result, last)
	}

	return result
}

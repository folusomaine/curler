package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	FileName        = ".postack.yaml"
	LegacyFileName  = ".curler.yaml"
	DefaultTimeout  = "30s"
	AuthTypeNone    = "none"
	AuthTypeBearer  = "bearer"
	AuthTypeBasic   = "basic"
	BodyModeNone    = "none"
	BodyModeJSON    = "json"
	BodyModeRaw     = "raw"
	ConfigVersionV1 = 1
)

var (
	ErrConfigNotFound = errors.New("postack config not found")
	ErrInvalidRef     = errors.New("request reference must be in collection/request form")
	varPattern        = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`)
)

type File struct {
	Version      int                          `yaml:"version"`
	ActiveEnv    string                       `yaml:"active_env"`
	Environments map[string]map[string]string `yaml:"environments,omitempty"`
	Collections  map[string]*Collection       `yaml:"collections,omitempty"`
}

type Collection struct {
	Requests map[string]*RequestDef `yaml:"requests,omitempty"`
}

type RequestDef struct {
	Method  string            `yaml:"method,omitempty"`
	URL     string            `yaml:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Query   map[string]string `yaml:"query,omitempty"`
	Auth    AuthConfig        `yaml:"auth,omitempty"`
	Body    BodyConfig        `yaml:"body,omitempty"`
	Timeout string            `yaml:"timeout,omitempty"`
}

type AuthConfig struct {
	Type     string `yaml:"type,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	Token    string `yaml:"token,omitempty"`
}

type BodyConfig struct {
	Mode    string `yaml:"mode,omitempty"`
	Content string `yaml:"content,omitempty"`
}

func Default() *File {
	return &File{
		Version:   ConfigVersionV1,
		ActiveEnv: "local",
		Environments: map[string]map[string]string{
			"local": {
				"BASE_URL":  "http://localhost:8080",
				"API_TOKEN": "local-demo-token",
			},
			"staging": {
				"BASE_URL":  "https://api.example.com",
				"API_TOKEN": "staging-demo-token",
			},
		},
		Collections: map[string]*Collection{
			"default": {
				Requests: map[string]*RequestDef{
					"users": {
						Method: "GET",
						URL:    "${BASE_URL}/users",
						Headers: map[string]string{
							"Accept": "application/json",
						},
						Auth: AuthConfig{
							Type:  AuthTypeBearer,
							Token: "${API_TOKEN}",
						},
						Timeout: DefaultTimeout,
					},
				},
			},
		},
	}
}

func FindPath(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	for {
		for _, name := range []string{FileName, LegacyFileName} {
			candidate := filepath.Join(dir, name)
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() {
				return candidate, nil
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrConfigNotFound
		}
		dir = parent
	}
}

func LoadFromDir(startDir string) (*File, string, error) {
	path, err := FindPath(startDir)
	if err != nil {
		return nil, "", err
	}
	file, err := LoadPath(path)
	if err != nil {
		return nil, "", err
	}
	return file, path, nil
}

func LoadPath(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var file File
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	file.Normalize()
	return &file, nil
}

func SavePath(path string, file *File) error {
	file.Normalize()
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (f *File) Normalize() {
	if f.Version == 0 {
		f.Version = ConfigVersionV1
	}
	if f.Environments == nil {
		f.Environments = map[string]map[string]string{}
	}
	if f.Collections == nil {
		f.Collections = map[string]*Collection{}
	}
	if f.ActiveEnv == "" {
		for envName := range f.Environments {
			f.ActiveEnv = envName
			break
		}
	}
	for collectionName, collection := range f.Collections {
		if collection == nil {
			collection = &Collection{}
			f.Collections[collectionName] = collection
		}
		if collection.Requests == nil {
			collection.Requests = map[string]*RequestDef{}
		}
		for requestName, request := range collection.Requests {
			if request == nil {
				request = &RequestDef{}
				collection.Requests[requestName] = request
			}
			request.Normalize()
		}
	}
}

func (r *RequestDef) Normalize() {
	if r.Method == "" {
		r.Method = "GET"
	}
	if r.Timeout == "" {
		r.Timeout = DefaultTimeout
	}
	if r.Headers == nil {
		r.Headers = map[string]string{}
	}
	if r.Query == nil {
		r.Query = map[string]string{}
	}
	if r.Auth.Type == "" {
		r.Auth.Type = AuthTypeNone
	}
	if r.Body.Mode == "" {
		r.Body.Mode = BodyModeNone
	}
}

func (f *File) ListRefs() []string {
	refs := make([]string, 0)
	for collectionName, collection := range f.Collections {
		for requestName := range collection.Requests {
			refs = append(refs, collectionName+"/"+requestName)
		}
	}
	sort.Strings(refs)
	return refs
}

func (f *File) Environment(name string) (map[string]string, error) {
	if name == "" {
		return nil, errors.New("environment name is required")
	}
	values, ok := f.Environments[name]
	if !ok {
		return nil, fmt.Errorf("environment %q not found", name)
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned, nil
}

func (f *File) GetRequest(ref string) (string, string, *RequestDef, error) {
	parts := strings.Split(ref, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", nil, ErrInvalidRef
	}
	collection, ok := f.Collections[parts[0]]
	if !ok {
		return "", "", nil, fmt.Errorf("collection %q not found", parts[0])
	}
	request, ok := collection.Requests[parts[1]]
	if !ok {
		return "", "", nil, fmt.Errorf("request %q not found in collection %q", parts[1], parts[0])
	}
	cloned := CloneRequest(request)
	return parts[0], parts[1], cloned, nil
}

func (f *File) UpsertRequest(collectionName, requestName string, request *RequestDef) {
	if f.Collections == nil {
		f.Collections = map[string]*Collection{}
	}
	collection := f.Collections[collectionName]
	if collection == nil {
		collection = &Collection{Requests: map[string]*RequestDef{}}
		f.Collections[collectionName] = collection
	}
	if collection.Requests == nil {
		collection.Requests = map[string]*RequestDef{}
	}
	collection.Requests[requestName] = CloneRequest(request)
}

func (f *File) DeleteRequest(collectionName, requestName string) {
	collection := f.Collections[collectionName]
	if collection == nil || collection.Requests == nil {
		return
	}
	delete(collection.Requests, requestName)
	if len(collection.Requests) == 0 {
		delete(f.Collections, collectionName)
	}
}

func CloneRequest(request *RequestDef) *RequestDef {
	if request == nil {
		return &RequestDef{}
	}
	cloned := *request
	cloned.Headers = cloneStringMap(request.Headers)
	cloned.Query = cloneStringMap(request.Query)
	cloned.Normalize()
	return &cloned
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func InterpolateString(value string, env map[string]string) (string, error) {
	missing := map[string]struct{}{}
	expanded := varPattern.ReplaceAllStringFunc(value, func(match string) string {
		name := varPattern.FindStringSubmatch(match)[1]
		resolved, ok := env[name]
		if !ok {
			missing[name] = struct{}{}
			return match
		}
		return resolved
	})
	if len(missing) == 0 {
		return expanded, nil
	}
	names := make([]string, 0, len(missing))
	for name := range missing {
		names = append(names, name)
	}
	sort.Strings(names)
	return "", fmt.Errorf("missing environment values: %s", strings.Join(names, ", "))
}

func ValidateAuthType(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AuthTypeNone, AuthTypeBearer, AuthTypeBasic:
		return nil
	default:
		return fmt.Errorf("unsupported auth type %q", value)
	}
}

func ValidateBodyMode(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case BodyModeNone, BodyModeJSON, BodyModeRaw:
		return nil
	default:
		return fmt.Errorf("unsupported body mode %q", value)
	}
}

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"curler/internal/config"
	"curler/internal/executor"
	"curler/internal/format"
	"curler/internal/tui"
)

type App struct {
	stdout  io.Writer
	stderr  io.Writer
	cwd     string
	runTUI  func(path string, file *config.File) error
	execute func(ctx context.Context, request *executor.ResolvedRequest) (*executor.Response, error)
}

func Run(args []string, stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "failed to determine working directory: %v\n", err)
		return 1
	}
	app := &App{
		stdout:  stdout,
		stderr:  stderr,
		cwd:     cwd,
		runTUI:  tui.Run,
		execute: executor.Execute,
	}
	return app.Run(args)
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		return a.runTUICommand()
	}

	switch args[0] {
	case "init":
		return a.runInit(args[1:])
	case "list":
		return a.runList(args[1:])
	case "run":
		return a.runRun(args[1:])
	default:
		fmt.Fprintf(a.stderr, "unknown command %q\n", args[0])
		return 1
	}
}

func (a *App) runInit(args []string) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	if err := flags.Parse(args); err != nil {
		return 1
	}

	path := filepath.Join(a.cwd, config.FileName)
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(a.stderr, "%s already exists\n", path)
		return 1
	} else if !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(a.stderr, "failed to inspect %s: %v\n", path, err)
		return 1
	}

	if err := config.SavePath(path, config.Default()); err != nil {
		fmt.Fprintf(a.stderr, "failed to write %s: %v\n", path, err)
		return 1
	}

	fmt.Fprintf(a.stdout, "created %s\n", path)
	return 0
}

func (a *App) runList(args []string) int {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	if err := flags.Parse(args); err != nil {
		return 1
	}

	file, _, err := config.LoadFromDir(a.cwd)
	if err != nil {
		if errors.Is(err, config.ErrConfigNotFound) {
			fmt.Fprintf(a.stderr, "no %s found. Run `curler init` first.\n", config.FileName)
			return 1
		}
		fmt.Fprintf(a.stderr, "failed to load config: %v\n", err)
		return 1
	}

	refs := file.ListRefs()
	for _, ref := range refs {
		fmt.Fprintln(a.stdout, ref)
	}
	return 0
}

func (a *App) runRun(args []string) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(a.stderr)

	var envName string
	var headerValues multiValueFlag
	var queryValues multiValueFlag
	var body string
	var timeout string
	var outputPath string

	flags.StringVar(&envName, "env", "", "environment name")
	flags.Var(&headerValues, "header", "repeatable header override in Key: Value form")
	flags.Var(&queryValues, "query", "repeatable query override in key=value form")
	flags.StringVar(&body, "body", "", "raw body override")
	flags.StringVar(&timeout, "timeout", "", "timeout override (for example 5s)")
	flags.StringVar(&outputPath, "output", "", "write response body to a file")

	refArg := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		refArg = args[0]
		args = args[1:]
	}

	if err := flags.Parse(args); err != nil {
		return 1
	}
	if refArg == "" {
		if flags.NArg() != 1 {
			fmt.Fprintf(a.stderr, "usage: curler run <collection/request> [flags]\n")
			return 1
		}
		refArg = flags.Arg(0)
	}
	if flags.NArg() > 1 {
		fmt.Fprintf(a.stderr, "usage: curler run <collection/request> [flags]\n")
		return 1
	}

	file, _, err := config.LoadFromDir(a.cwd)
	if err != nil {
		fmt.Fprintf(a.stderr, "failed to load config: %v\n", err)
		return 1
	}

	_, _, request, err := file.GetRequest(refArg)
	if err != nil {
		fmt.Fprintf(a.stderr, "failed to resolve request: %v\n", err)
		return 1
	}

	if envName == "" {
		envName = file.ActiveEnv
	}
	env, err := file.Environment(envName)
	if err != nil {
		fmt.Fprintf(a.stderr, "failed to resolve environment: %v\n", err)
		return 1
	}

	headerOverrides, err := parseHeaderOverrides(headerValues.values)
	if err != nil {
		fmt.Fprintf(a.stderr, "invalid header override: %v\n", err)
		return 1
	}
	queryOverrides, err := parseQueryOverrides(queryValues.values)
	if err != nil {
		fmt.Fprintf(a.stderr, "invalid query override: %v\n", err)
		return 1
	}

	var timeoutOverride time.Duration
	if timeout != "" {
		timeoutOverride, err = time.ParseDuration(timeout)
		if err != nil {
			fmt.Fprintf(a.stderr, "invalid timeout: %v\n", err)
			return 1
		}
	}

	var bodyOverride *string
	if flags.Lookup("body").Value.String() != "" {
		bodyOverride = &body
	}

	resolved, err := executor.PrepareRequest(request, env, executor.OverrideOptions{
		Headers: headerOverrides,
		Query:   queryOverrides,
		Body:    bodyOverride,
		Timeout: timeoutOverride,
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "failed to prepare request: %v\n", err)
		return 1
	}

	response, err := a.execute(context.Background(), resolved)
	if err != nil {
		fmt.Fprintf(a.stderr, "request failed: %v\n", err)
		return 1
	}

	if outputPath != "" {
		if err := os.WriteFile(outputPath, response.Body, 0o644); err != nil {
			fmt.Fprintf(a.stderr, "failed to write response body: %v\n", err)
			return 1
		}
	}

	fmt.Fprintln(a.stdout, format.RenderResponse(response))
	if outputPath != "" {
		fmt.Fprintf(a.stdout, "\n\nsaved body to %s\n", outputPath)
	}
	return 0
}

func (a *App) runTUICommand() int {
	file, path, err := config.LoadFromDir(a.cwd)
	if err != nil {
		if errors.Is(err, config.ErrConfigNotFound) {
			fmt.Fprintf(a.stderr, "no %s found. Run `curler init` first.\n", config.FileName)
			return 1
		}
		fmt.Fprintf(a.stderr, "failed to load config: %v\n", err)
		return 1
	}

	if err := a.runTUI(path, file); err != nil {
		fmt.Fprintf(a.stderr, "tui error: %v\n", err)
		return 1
	}
	return 0
}

type multiValueFlag struct {
	values []string
}

func (f *multiValueFlag) String() string {
	return strings.Join(f.values, ",")
}

func (f *multiValueFlag) Set(value string) error {
	f.values = append(f.values, value)
	return nil
}

func parseHeaderOverrides(values []string) (map[string]string, error) {
	headers := map[string]string{}
	for _, value := range values {
		key, resolvedValue, err := splitPair(value, []string{":", "="})
		if err != nil {
			return nil, err
		}
		headers[key] = resolvedValue
	}
	return headers, nil
}

func parseQueryOverrides(values []string) (map[string]string, error) {
	query := map[string]string{}
	for _, value := range values {
		key, resolvedValue, err := splitPair(value, []string{"="})
		if err != nil {
			return nil, err
		}
		query[key] = resolvedValue
	}
	return query, nil
}

func splitPair(value string, separators []string) (string, string, error) {
	for _, separator := range separators {
		if index := strings.Index(value, separator); index >= 0 {
			key := strings.TrimSpace(value[:index])
			resolvedValue := strings.TrimSpace(value[index+len(separator):])
			if key == "" {
				return "", "", fmt.Errorf("missing key in %q", value)
			}
			return key, resolvedValue, nil
		}
	}
	sort.Strings(separators)
	return "", "", fmt.Errorf("expected separator %s in %q", strings.Join(separators, " or "), value)
}

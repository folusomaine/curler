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
	"text/tabwriter"
	"time"

	"postack/internal/config"
	"postack/internal/executor"
	"postack/internal/format"
	"postack/internal/tui"
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
	case "-h", "--help", "help":
		a.printHelp()
		return 0
	case "init", "i":
		return a.runInit(args[1:])
	case "list", "l":
		return a.runList(args[1:])
	case "run", "r":
		return a.runRun(args[1:])
	default:
		fmt.Fprintf(a.stderr, "unknown command %q\n", args[0])
		return 1
	}
}

func (a *App) printHelp() {
	fmt.Fprintln(a.stdout, "Usage:")
	fmt.Fprintln(a.stdout, "  postack <command> [arguments]")
	fmt.Fprintln(a.stdout)
	fmt.Fprintln(a.stdout, "Commands:")
	fmt.Fprintln(a.stdout, "  init, i    create a postack.yaml config in the current directory")
	fmt.Fprintln(a.stdout, "  list, l    list saved request references")
	fmt.Fprintln(a.stdout, "  run, r     run a saved request or `all`")
	fmt.Fprintln(a.stdout)
	fmt.Fprintln(a.stdout, "Top-level flags:")
	fmt.Fprintln(a.stdout, "  -h, --help    show this help summary")
	fmt.Fprintln(a.stdout)
	fmt.Fprintln(a.stdout, "Run examples:")
	fmt.Fprintln(a.stdout, "  postack run default/users")
	fmt.Fprintln(a.stdout, "  postack r all")
	fmt.Fprintln(a.stdout, "  postack r default/users -o response.json")
	fmt.Fprintln(a.stdout)
	fmt.Fprintln(a.stdout, "Run flags:")
	fmt.Fprintln(a.stdout, "  -e, --env")
	fmt.Fprintln(a.stdout, "  -H, --header")
	fmt.Fprintln(a.stdout, "  -q, --query")
	fmt.Fprintln(a.stdout, "  -b, --body")
	fmt.Fprintln(a.stdout, "  -t, --timeout")
	fmt.Fprintln(a.stdout, "  -o, --output")
	fmt.Fprintln(a.stdout, "  -s, --status-only")
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
			fmt.Fprintf(a.stderr, "no %s found. Run `postack init` first.\n", config.FileName)
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
	var statusOnly bool

	flags.StringVar(&envName, "env", "", "environment name")
	flags.StringVar(&envName, "e", "", "environment name (shorthand)")
	flags.Var(&headerValues, "header", "repeatable header override in Key: Value form")
	flags.Var(&headerValues, "H", "repeatable header override in Key: Value form (shorthand)")
	flags.Var(&queryValues, "query", "repeatable query override in key=value form")
	flags.Var(&queryValues, "q", "repeatable query override in key=value form (shorthand)")
	flags.StringVar(&body, "body", "", "raw body override")
	flags.StringVar(&body, "b", "", "raw body override (shorthand)")
	flags.StringVar(&timeout, "timeout", "", "timeout override (for example 5s)")
	flags.StringVar(&timeout, "t", "", "timeout override (for example 5s) (shorthand)")
	flags.StringVar(&outputPath, "output", "", "write response body to a file instead of stdout")
	flags.StringVar(&outputPath, "o", "", "write response body to a file instead of stdout (shorthand)")
	flags.BoolVar(&statusOnly, "status-only", false, "print only the response status code")
	flags.BoolVar(&statusOnly, "s", false, "print only the response status code (shorthand)")

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
			fmt.Fprintf(a.stderr, "usage: postack run <collection/request> [flags]\n")
			return 1
		}
		refArg = flags.Arg(0)
	}
	if flags.NArg() > 1 {
		fmt.Fprintf(a.stderr, "usage: postack run <collection/request> [flags]\n")
		return 1
	}

	file, _, err := config.LoadFromDir(a.cwd)
	if err != nil {
		fmt.Fprintf(a.stderr, "failed to load config: %v\n", err)
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

	if refArg == "all" {
		return a.runAllRequests(file, env)
	}

	_, _, request, err := file.GetRequest(refArg)
	if err != nil {
		fmt.Fprintf(a.stderr, "failed to resolve request: %v\n", err)
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
		fmt.Fprintln(a.stdout, outputPath)
		return 0
	}

	if statusOnly {
		fmt.Fprintln(a.stdout, response.StatusCode)
	} else {
		fmt.Fprintln(a.stdout, format.RenderResponse(response))
	}
	return 0
}

type runAllResult struct {
	endpoint string
	result   string
}

func (a *App) runAllRequests(file *config.File, env map[string]string) int {
	refs := file.ListRefs()
	results := make([]runAllResult, 0, len(refs))

	for _, ref := range refs {
		_, _, request, err := file.GetRequest(ref)
		if err != nil {
			results = append(results, runAllResult{
				endpoint: ref,
				result:   "error: " + err.Error(),
			})
			continue
		}

		resolved, err := executor.PrepareRequest(request, env, executor.OverrideOptions{})
		if err != nil {
			results = append(results, runAllResult{
				endpoint: ref,
				result:   "error: " + err.Error(),
			})
			continue
		}

		response, err := a.execute(context.Background(), resolved)
		if err != nil {
			results = append(results, runAllResult{
				endpoint: ref,
				result:   "error: " + err.Error(),
			})
			continue
		}

		results = append(results, runAllResult{
			endpoint: ref,
			result:   fmt.Sprintf("%d", response.StatusCode),
		})
	}

	fmt.Fprintln(a.stdout, renderRunAllResults(results))
	return 0
}

func renderRunAllResults(results []runAllResult) string {
	var builder strings.Builder
	writer := tabwriter.NewWriter(&builder, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "ENDPOINT\tRESULT")
	for _, result := range results {
		fmt.Fprintf(writer, "%s\t%s\n", result.endpoint, result.result)
	}
	_ = writer.Flush()
	return strings.TrimSpace(builder.String())
}

func (a *App) runTUICommand() int {
	file, path, err := config.LoadFromDir(a.cwd)
	if err != nil {
		if errors.Is(err, config.ErrConfigNotFound) {
			fmt.Fprintf(a.stderr, "no %s found. Run `postack init` first.\n", config.FileName)
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

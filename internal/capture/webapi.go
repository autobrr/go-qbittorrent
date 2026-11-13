package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing/fstest"
)

// webAPITags are the releases that testdata/features/ probes.
var webAPITags = []string{"release-4.3.9", "release-4.4.0", "release-4.4.5", "release-4.5.0", "release-4.5.5",
	"release-4.6.0", "release-4.6.7", "release-5.0.4", "release-5.1.4", "release-5.2.4", "release-5.3.0"}

type release struct {
	Actions       map[string]action `json:"actions"`
	Version       string            `json:"version"`
	WebAPIVersion string            `json:"webapi_version"`
}

type action struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
}

var (
	apiVersionRe = regexp.MustCompile(`API_VERSION\s*\{([\d,\s]+)\}`)
	// {{u"app"_s, u"shutdown"_s}, Http::HEADER_REQUEST_METHOD_POST}
	// {{QLatin1String("app"), QLatin1String("shutdown")}, Http::METHOD_POST}
	postOnlyRe = regexp.MustCompile(`\{\{[^"]*"(\w+)"[^"]*"(\w+)"[^}]*\},\s*Http::\w*POST\}`)
	postRe     = regexp.MustCompile(`Http::\w*POST`)
	declRe     = regexp.MustCompile(`^\s*void (\w+)Action\(\)`)
	defRe      = regexp.MustCompile(`^void \w+::(\w+)Action\(\)`)
	literalRe  = regexp.MustCompile(`"([^"]*)"`)
	requireRe  = regexp.MustCompile(`requireParams\(\{(.*)\}\);`)
	// const QString KEY_TASK_ID = u"taskID"_s;
	constRe = regexp.MustCompile(`(?m)^\s*const\s+QString\s+(\w+)\s*=\s*u?"([^"]*)"`)
)

const (
	qbtRepo   = "https://github.com/qbittorrent/qBittorrent.git"
	appHeader = "src/webui/webapplication.h"
	apiDir    = "src/webui/api"
)

// parseRelease reads the actions of one release from its source tree. The
// controller in <scope>controller.h serves the actions of <scope>.
func parseRelease(tree fs.FS, tag string) (release, error) {
	r := release{Version: "v" + strings.TrimPrefix(tag, "release-"), Actions: map[string]action{}}

	app, err := fs.ReadFile(tree, appHeader)
	if err != nil {
		return r, err
	}
	m := apiVersionRe.FindSubmatch(app)
	if m == nil {
		return r, fmt.Errorf("%s: no API_VERSION", tag)
	}
	r.WebAPIVersion = strings.Join(strings.Fields(strings.ReplaceAll(string(m[1]), ",", " ")), ".")
	// Releases before 4.4.5 have no method table, so every action is "any".
	postOnly := map[string]bool{}
	entries := postOnlyRe.FindAllSubmatch(app, -1)
	for _, m := range entries {
		postOnly[string(m[1])+"/"+string(m[2])] = true
	}
	if n := len(postRe.FindAll(app, -1)); len(entries) != n {
		return r, fmt.Errorf("%s: read %d of %d POST entries in m_allowedMethod", tag, len(entries), n)
	}
	if len(postOnly) == 0 && bytes.Contains(app, []byte("m_allowedMethod")) {
		return r, fmt.Errorf("%s: no POST entry in m_allowedMethod", tag)
	}

	headers, err := fs.Glob(tree, apiDir+"/*controller.h")
	if err != nil {
		return r, err
	}
	for _, header := range headers {
		scope := strings.TrimSuffix(path.Base(header), "controller.h")
		decls, err := fs.ReadFile(tree, header)
		if err != nil {
			return r, err
		}
		found := false
		for line := range strings.Lines(string(decls)) {
			if !strings.Contains(line, "Action(") {
				continue
			}
			m := declRe.FindStringSubmatch(line)
			if m == nil {
				return r, fmt.Errorf("%s: %s: cannot read action declaration: %s", tag, header, strings.TrimSpace(line))
			}
			found = true
			name := scope + "/" + m[1]
			method := "any"
			if postOnly[name] {
				method = "POST"
			}
			r.Actions[name] = action{Method: method, Params: []string{}}
		}
		// apicontroller.h is the base class: it has no actions.
		if !found {
			continue
		}
		if err := parseRequiredParams(tree, strings.TrimSuffix(header, ".h")+".cpp", scope, r.Actions); err != nil {
			return r, fmt.Errorf("%s: %w", tag, err)
		}
	}
	for name := range postOnly {
		if _, ok := r.Actions[name]; !ok {
			return r, fmt.Errorf("%s: m_allowedMethod lists %s, which no controller declares", tag, name)
		}
	}
	return r, nil
}

// parseRequiredParams fills Params from the one requireParams line in the body
// of each action. A name is a string literal or a QString constant of the same file.
func parseRequiredParams(tree fs.FS, file, scope string, actions map[string]action) error {
	body, err := fs.ReadFile(tree, file)
	if err != nil {
		return err
	}
	consts := map[string]string{}
	for _, m := range constRe.FindAllStringSubmatch(string(body), -1) {
		consts[m[1]] = m[2]
	}
	called := map[string]bool{}
	current := ""
	for line := range strings.Lines(string(body)) {
		if m := defRe.FindStringSubmatch(line); m != nil {
			current = scope + "/" + m[1]
			continue
		}
		if strings.HasPrefix(line, "}") {
			current = ""
			continue
		}
		if !strings.Contains(line, "requireParams(") {
			continue
		}
		a, ok := actions[current]
		if !ok {
			return fmt.Errorf("%s: requireParams outside an action: %s", file, strings.TrimSpace(line))
		}
		if called[current] {
			return fmt.Errorf("%s: %s calls requireParams twice", file, current)
		}
		called[current] = true
		args := requireRe.FindStringSubmatch(line)
		if args == nil {
			return fmt.Errorf("%s: requireParams of %s is not on one line: %s", file, current, strings.TrimSpace(line))
		}
		for arg := range strings.SplitSeq(args[1], ",") {
			arg = strings.TrimSpace(arg)
			if m := literalRe.FindStringSubmatch(arg); m != nil {
				a.Params = append(a.Params, m[1])
			} else if name, ok := consts[arg]; ok {
				a.Params = append(a.Params, name)
			} else {
				return fmt.Errorf("%s: %s passes %q to requireParams", file, current, arg)
			}
		}
		actions[current] = a
	}
	return nil
}

// captureWebAPI writes one file per release to testdata/webapi/.
func captureWebAPI() error {
	src := os.Getenv("QBT_SRC")
	if src == "" {
		tmp, err := os.MkdirTemp("", "qbt-src-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		src = tmp
		if _, err := git(src, "clone", "--quiet", "--filter=blob:none", "--no-checkout", qbtRepo, "."); err != nil {
			return err
		}
	}

	// Write nothing if any release fails.
	captures := map[string][]byte{}
	for _, tag := range webAPITags {
		names, err := git(src, "ls-tree", "--name-only", tag+":"+apiDir)
		if err != nil {
			return err
		}
		files := []string{appHeader}
		for name := range strings.FieldsSeq(string(names)) {
			if strings.HasSuffix(name, "controller.h") || strings.HasSuffix(name, "controller.cpp") {
				files = append(files, path.Join(apiDir, name))
			}
		}
		tree := fstest.MapFS{}
		for _, file := range files {
			data, err := git(src, "show", tag+":"+file)
			if err != nil {
				return err
			}
			tree[file] = &fstest.MapFile{Data: data}
		}
		r, err := parseRelease(tree, tag)
		if err != nil {
			return err
		}
		out, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		captures[strings.TrimPrefix(r.Version, "v")+".json"] = append(out, '\n')
		fmt.Printf("%s (Web API %s): %d actions\n", r.Version, r.WebAPIVersion, len(r.Actions))
	}

	dir := filepath.Join("..", "..", "testdata", "webapi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, data := range captures {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func git(dir string, args ...string) ([]byte, error) {
	return run("git", append([]string{"-C", dir}, args...)...)
}

func run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

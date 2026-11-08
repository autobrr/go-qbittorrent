package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// containerName is the one qBittorrent container that the command runs at a time.
var containerName = fmt.Sprintf("qbt-capture-%d", os.Getpid())

// qBittorrent 4.6.1 and later print a temporary password. Older versions use adminadmin.
var passwordRe = regexp.MustCompile(`temporary password is provided for this session: (\S+)`)

// container is a logged-in qBittorrent. Requests run curl inside the container,
// so the container can run with no network.
type container struct {
	image string
	url   string
}

// image returns the linuxserver image of a release. 14.3.9 is the linuxserver
// tag of qBittorrent 4.3.9.
func image(version string) string {
	if version == "4.3.9" {
		version = "14.3.9"
	}
	return "lscr.io/linuxserver/qbittorrent:" + version
}

func startContainer(image string, port int, args ...string) (*container, error) {
	removeContainer()
	argv := append([]string{"run", "-d", "--rm", "--name", containerName, "-e", fmt.Sprintf("WEBUI_PORT=%d", port)}, args...)
	if _, err := docker(append(argv, image)...); err != nil {
		return nil, err
	}
	c := &container{image: image, url: fmt.Sprintf("http://127.0.0.1:%d", port)}
	for range 60 {
		password := "adminadmin"
		logs, _ := exec.Command("docker", "logs", containerName).CombinedOutput()
		if m := passwordRe.FindAllSubmatch(logs, -1); m != nil {
			password = string(m[len(m)-1][1])
		}
		// qBittorrent 5.3 answers a login with 204 and no body, so look for the session cookie.
		_, err := docker("exec", containerName, "curl", "-fsS", "--max-time", "2", "-c", "/tmp/cookies",
			"-H", "Referer: "+c.url, "--data-urlencode", "username=admin",
			"--data-urlencode", "password="+password, c.url+"/api/v2/auth/login")
		if err == nil {
			if _, err := docker("exec", containerName, "grep", "-q", "SID", "/tmp/cookies"); err == nil {
				return c, nil
			}
		}
		time.Sleep(time.Second)
	}
	removeContainer()
	return nil, fmt.Errorf("%s: login failed", image)
}

func removeContainer() {
	_ = exec.Command("docker", "rm", "-f", containerName).Run()
}

func (c *container) api(action string, args ...string) ([]byte, error) {
	return docker(append(c.curl(action, "-f"), args...)...)
}

func (c *container) status(action string, args ...string) (int, error) {
	out, err := docker(append(c.curl(action, "-o", "/dev/null", "-w", "%{http_code}"), args...)...)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(out))
}

func (c *container) curl(action string, args ...string) []string {
	return append([]string{"exec", containerName, "curl", "-sS", "--max-time", "5", "-b", "/tmp/cookies",
		"-H", "Referer: " + c.url, c.url + "/api/v2/" + action}, args...)
}

func (c *container) getJSON(v any, action string, args ...string) error {
	out, err := c.api(action, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func (c *container) digest() (string, error) {
	out, err := docker("image", "inspect", c.image, "--format", "{{index .RepoDigests 0}}")
	return strings.TrimSpace(string(out)), err
}

func docker(args ...string) ([]byte, error) { return run("docker", args...) }

// mkbrr writes a torrent of a zero-filled file of the given size, with
// 64 KiB pieces, and limits the hashing as the testing rules ask.
func mkbrr(content string, size int64, args ...string) error {
	if err := os.WriteFile(content, nil, 0o644); err != nil {
		return err
	}
	if err := os.Truncate(content, size); err != nil {
		return err
	}
	return mkbrrCreate(content, strings.TrimSuffix(content, filepath.Ext(content))+".torrent",
		append([]string{"--piece-length", "16"}, args...)...)
}

// mkbrrCreate writes a torrent of content to output, and limits the hashing as
// the testing rules ask.
func mkbrrCreate(content, output string, args ...string) error {
	cmd := exec.Command("nice", append([]string{"-n", "19", "env", "GOMAXPROCS=2", "mkbrr", "create", content,
		"--workers", "1", "--no-creator", "--quiet", "--output", output}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mkbrr: %w: %s", err, out)
	}
	return nil
}

// cleanups run last first, on success, on an error, and on an interrupt.
var cleanups struct {
	sync.Mutex
	fns []func()
}

func onCleanup(fn func()) {
	cleanups.Lock()
	defer cleanups.Unlock()
	cleanups.fns = append(cleanups.fns, fn)
}

func runCleanups() {
	cleanups.Lock()
	defer cleanups.Unlock()
	for _, fn := range slices.Backward(cleanups.fns) {
		fn()
	}
	cleanups.fns = nil
}

// writeSorted writes data to testdata/<dir>/<name> with sorted keys and
// two-space indent, the output of jq -S.
func writeSorted(dir, name string, data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(v); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("..", "..", "testdata", dir, name), buf.Bytes(), 0o644)
}

func poll(tries int, ready func() (bool, error)) (bool, error) {
	for range tries {
		ok, err := ready()
		if ok || err != nil {
			return ok, err
		}
		time.Sleep(time.Second)
	}
	return false, nil
}

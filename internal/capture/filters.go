package main

import (
	"cmp"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"go/version"
	"io"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

var filterVersions = []string{"4.3.9", "4.5.5", "4.6.7", "5.0.4", "5.1.4", "5.2.4", "5.3.0"}

// knownStates is KnownTorrentStates() of go-qbittorrent. This command is its own
// module, so it keeps a copy. The library test fails when a capture has no
// result for one of these names.
var knownStates = []string{"allocating", "checkingDL", "checkingResumeData", "checkingUP", "downloading",
	"error", "forcedDL", "forcedMetaDL", "forcedUP", "metaDL", "missingFiles", "moving", "pausedDL", "pausedUP",
	"queuedDL", "queuedUP", "stalledDL", "stalledUP", "stoppedDL", "stoppedUP", "unknown", "uploading"}

// filterValues are the filter names that a release from 4.3.9 to 5.3.0 accepts,
// other names that callers send, the state names, the Tracker health names of
// qui, and one made-up name.
var filterValues = func() []string {
	v := append([]string{"all", "downloading", "seeding", "completed", "paused", "resumed", "stopped", "running",
		"active", "inactive", "stalled", "stalled_uploading", "stalled_downloading", "checking", "moving", "errored",
		"uploading", "stalled_seeding", "unregistered", "tracker_down", "tracker_error", "not_a_filter"}, knownStates...)
	slices.Sort(v)
	return slices.Compact(v)
}()

// A seed is one made-up torrent with no tracker and random content. A recheck
// of zeros ends too fast to see checkingUP.
type seed struct {
	size     int64
	pieceExp int
	files    int // a folder of this many files of size bytes each, when not 0
}

var seeds = func() map[string]seed {
	s := map[string]seed{
		"b-up": {60 << 20, 18, 0}, "b-dl": {60 << 20, 18, 0},
		"b-check": {100 << 20, 18, 0}, "b-checkdl": {100 << 20, 18, 0},
		// moving lasts long enough to see only with many small files.
		"b-move": {8192, 16, 3000},
	}
	for _, n := range []string{"s-stalled", "s-stopped", "s-forced", "s-q1", "s-q2", "s-q3", "l-stopped", "l-stalled",
		"l-forced", "l-q1", "l-q2", "l-q3", "s-missing", "s-missing-stopped", "s-error", "s-unreadable",
		"c-movies", "c-hd", "c-deep", "c-sibling", "c-none"} {
		s[n] = seed{256 << 10, 16, 0}
	}
	return s
}()

func writeSeed(name, dir string) error {
	s := seeds[name]
	path := filepath.Join(dir, "probe-"+name)
	if s.files == 0 {
		return randomFile(path, s.size)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		return err
	}
	for i := range s.files {
		if err := randomFile(filepath.Join(path, fmt.Sprintf("f%04d", i)), s.size); err != nil {
			return err
		}
	}
	return nil
}

func randomFile(path string, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = io.CopyN(f, rand.Reader, size)
	return errors.Join(err, f.Close())
}

func (r *rig) place(name, dir string) error {
	src, dst := filepath.Join(r.seeds, "probe-"+name), filepath.Join(dir, "probe-"+name)
	if seeds[name].files > 0 {
		return os.CopyFS(dst, os.DirFS(src))
	}
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, body, 0o644)
}

// captureFilters writes the torrents/info results of each filter value, each
// category, and each tag to testdata/filters/, for each release or for the given
// releases. Container A runs with no network. Container B shares only the
// loopback of A, and seeds to A and from A.
func captureFilters(scratch string, versions []string) error {
	if len(versions) == 0 {
		versions = filterVersions
	}
	if os.Getuid() == 0 {
		return errors.New("filters: run as a user that is not root, because chmod 000 has no effect for root")
	}
	seedDir := filepath.Join(scratch, "seed")
	torrents := filepath.Join(scratch, "torrents")
	for _, d := range []string{seedDir, torrents} {
		if err := os.Mkdir(d, 0o755); err != nil {
			return err
		}
	}
	for _, name := range slices.Sorted(maps.Keys(seeds)) {
		if err := writeSeed(name, seedDir); err != nil {
			return err
		}
		if err := mkbrrCreate(filepath.Join(seedDir, "probe-"+name), filepath.Join(torrents, "probe-"+name+".torrent"),
			"--piece-length", strconv.Itoa(seeds[name].pieceExp), "--private=false"); err != nil {
			return err
		}
	}
	// Keep the previous captures if any release fails.
	captures := map[string][]byte{}
	for _, v := range versions {
		r := &rig{want: v, root: filepath.Join(scratch, v), seeds: seedDir, torrents: torrents,
			a: fmt.Sprintf("qbt-filters-%d-a", os.Getpid()), b: fmt.Sprintf("qbt-filters-%d-b", os.Getpid())}
		capture, err := r.run()
		runCleanups()
		if err != nil {
			return fmt.Errorf("%s: %w", v, err)
		}
		captures[v+".json"] = capture
	}
	for name, data := range captures {
		if err := writeSorted("filters", name, data); err != nil {
			return err
		}
	}
	return nil
}

type rig struct {
	want, root, seeds, torrents string
	a, b                        string // container names
	major                       int
	// tallies[filter][state] tells if a torrent in the state was returned, and
	// if one was left out.
	tallies map[string]map[string]*[2]bool
	partial map[string]bool // the filter left out a torrent at least once
}

const (
	portA = 8080
	portB = 8081
)

func (r *rig) run() ([]byte, error) {
	onCleanup(func() { os.RemoveAll(r.root) })
	r.tallies = map[string]map[string]*[2]bool{}
	r.partial = map[string]bool{}
	for _, d := range []string{"a", "b/out", "data", "datab", "move"} {
		if err := os.MkdirAll(filepath.Join(r.root, d), 0o755); err != nil {
			return nil, err
		}
	}
	for profile, port := range map[string]int{"a": 6881, "b": 6882} {
		if err := r.writeConf(profile, port); err != nil {
			return nil, err
		}
	}
	img := image(r.want)
	user := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	ro := r.torrents + ":/torrents:ro"
	onCleanup(func() { _ = exec.Command("docker", "rm", "-f", r.b, r.a).Run() })
	if _, err := docker("run", "-d", "--name", r.a, "--network", "none", "--user", user, "-e", "HOME=/a",
		"-v", r.root+"/a:/a", "-v", r.root+"/data:/data", "-v", r.root+"/move:/move", "-v", ro,
		"--entrypoint", "tail", img, "-f", "/dev/null"); err != nil {
		return nil, err
	}
	if _, err := docker("run", "-d", "--name", r.b, "--network", "container:"+r.a, "--user", user, "-e", "HOME=/b",
		"-v", r.root+"/b:/b", "-v", r.root+"/datab:/datab", "-v", ro,
		"--entrypoint", "tail", img, "-f", "/dev/null"); err != nil {
		return nil, err
	}
	for c, port := range map[string]int{r.a: portA, r.b: portB} {
		if err := r.start(c, port); err != nil {
			return nil, err
		}
	}
	got, err := r.api(portA, "app/version")
	if err != nil {
		return nil, err
	}
	if got != "v"+r.want {
		return nil, fmt.Errorf("expected v%s, got %s", r.want, got)
	}
	if r.major, err = strconv.Atoi(strings.SplitN(r.want, ".", 2)[0]); err != nil {
		return nil, err
	}
	webapi, err := r.api(portA, "app/webapiVersion")
	if err != nil {
		return nil, err
	}
	for port, listen := range map[int]int{portA: 6881, portB: 6882} {
		prefs := fmt.Sprintf(`{"dht":false,"pex":false,"lsd":false,"upnp":false,"random_port":false,`+
			`"queueing_enabled":false,"bittorrent_protocol":1,"limit_lan_peers":true,"listen_port":%d}`, listen)
		if _, err := r.api(port, "app/setPreferences", "json="+prefs); err != nil {
			return nil, err
		}
	}
	time.Sleep(2 * time.Second)

	categories, tags, err := r.categoriesAndTags()
	if err != nil {
		return nil, err
	}
	for _, phase := range []func() error{r.steady, r.queued, r.checking, r.moving, r.faults} {
		if err := phase(); err != nil {
			return nil, err
		}
	}

	verdicts := map[string]map[string]string{}
	var reached []string
	for f, states := range r.tallies {
		verdicts[f] = map[string]string{}
		for s, t := range states {
			verdicts[f][s] = map[[2]bool]string{{true, false}: "yes", {false, true}: "no", {true, true}: "mixed"}[*t]
			if !slices.Contains(reached, s) {
				reached = append(reached, s)
			}
		}
	}
	var every []string
	for _, f := range filterValues {
		if !r.partial[f] {
			every = append(every, f)
		}
	}
	digest, err := docker("image", "inspect", img, "--format", "{{index .RepoDigests 0}}")
	if err != nil {
		return nil, err
	}
	slices.Sort(reached)
	fmt.Printf("%s (Web API %s): states reached %v\n", got, webapi, reached)
	return json.Marshal(map[string]any{
		"version": got, "webapi_version": webapi, "image": strings.TrimSpace(string(digest)), "network": "none",
		"filters": filterValues, "returned_every_torrent": every, "verdicts": verdicts,
		"categories": categories, "tags": tags,
	})
}

func (r *rig) writeConf(profile string, port int) error {
	dir := filepath.Join(r.root, profile, "qBittorrent", "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	conf := fmt.Sprintf("[LegalNotice]\nAccepted=true\n\n[BitTorrent]\nSession\\Port=%d\nSession\\DHTEnabled=false\n"+
		"Session\\LSDEnabled=false\nSession\\PeXEnabled=false\n\n[Preferences]\nWebUI\\LocalHostAuth=false\n", port)
	return os.WriteFile(filepath.Join(dir, "qBittorrent.conf"), []byte(conf), 0o644)
}

// setConf sets one key in the [BitTorrent] section of the profile of A, while
// qBittorrent is down.
func (r *rig) setConf(key, value string) error {
	path := filepath.Join(r.root, "a", "qBittorrent", "config", "qBittorrent.conf")
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var lines []string
	for line := range strings.Lines(string(body)) {
		if !strings.HasPrefix(line, key+"=") {
			lines = append(lines, line)
		}
	}
	text := strings.Replace(strings.Join(lines, ""), "[BitTorrent]\n", "[BitTorrent]\n"+key+"="+value+"\n", 1)
	return os.WriteFile(path, []byte(text), 0o644)
}

func (r *rig) start(c string, port int) error {
	profile := "/a"
	if c == r.b {
		profile = "/b"
	}
	if _, err := docker("exec", "-d", c, "sh", "-c", fmt.Sprintf(
		"PATH=/app:$PATH; exec qbittorrent-nox --profile=%s --webui-port=%d >>%s/stdout.log 2>&1", profile, port, profile)); err != nil {
		return err
	}
	up, err := poll(240, func() (bool, error) {
		code, _, _ := r.call(port, false, "app/version")
		return code == 200, nil
	})
	if err == nil && !up {
		err = fmt.Errorf("the WebUI on port %d did not start", port)
	}
	return err
}

func (r *rig) stopA() error {
	_, _, _ = r.call(portA, false, "app/shutdown", "x=1")
	_, err := docker("exec", r.a, "sh", "-c",
		"for i in $(seq 1 120); do pidof qbittorrent-nox >/dev/null || exit 0; sleep 0.5; done; exit 1")
	return err
}

// call sends one request from container B over the loopback. fields are
// key=value pairs, sent as a form when form is true.
func (r *rig) call(port int, form bool, action string, fields ...string) (int, string, error) {
	args := []string{"exec", r.b, "curl", "-sS", "--max-time", "90", "-w", "\n%{http_code}",
		"-H", fmt.Sprintf("Referer: http://127.0.0.1:%d", port)}
	for _, f := range fields {
		if form {
			args = append(args, "-F", f)
		} else {
			args = append(args, "--data-urlencode", f)
		}
	}
	out, err := docker(append(args, fmt.Sprintf("http://127.0.0.1:%d/api/v2/%s", port, action))...)
	if err != nil {
		return 0, "", err
	}
	body, code := "", string(out)
	if i := strings.LastIndexByte(code, '\n'); i >= 0 {
		body, code = code[:i], code[i+1:]
	}
	n, err := strconv.Atoi(strings.TrimSpace(code))
	return n, strings.TrimSpace(body), err
}

func (r *rig) api(port int, action string, fields ...string) (string, error) {
	return r.request(port, false, action, fields...)
}

func (r *rig) request(port int, form bool, action string, fields ...string) (string, error) {
	code, body, err := r.call(port, form, action, fields...)
	if err != nil {
		return "", err
	}
	if code != 200 && code != 204 {
		return "", fmt.Errorf("%s %v on port %d: HTTP %d: %.200s", action, fields, port, code, body)
	}
	return body, nil
}

type row struct {
	Name     string `json:"name"`
	Hash     string `json:"hash"`
	State    string `json:"state"`
	Category string `json:"category"`
	Tags     string `json:"tags"`
}

func (r *rig) info(port int, query string) (map[string]row, error) {
	body, err := r.api(port, "torrents/info"+query)
	if err != nil {
		return nil, err
	}
	return rowsByName([]byte(body))
}

func rowsByName(body []byte) (map[string]row, error) {
	var rows []row
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	m := make(map[string]row, len(rows))
	for _, t := range rows {
		m[t.Name] = t
	}
	return m, nil
}

// add adds a seed to the qBittorrent on port, stopped unless start is set, and
// returns its hash.
func (r *rig) add(name string, port int, save string, start, skipCheck bool, extra ...string) (string, error) {
	stopped := strconv.FormatBool(!start)
	fields := append([]string{"torrents=@/torrents/probe-" + name + ".torrent", "savepath=" + save, "autoTMM=false",
		"paused=" + stopped, "stopped=" + stopped, "skip_checking=" + strconv.FormatBool(skipCheck)}, extra...)
	if _, err := r.request(port, true, "torrents/add", fields...); err != nil {
		return "", err
	}
	return r.hashOf("probe-"+name, port)
}

func (r *rig) addMagnet(name, hash string, force bool) error {
	if _, err := r.request(portA, true, "torrents/add", "urls=magnet:?xt=urn:btih:"+hash+"&dn=probe-"+name,
		"savepath=/data", "autoTMM=false"); err != nil {
		return err
	}
	if _, err := r.hashOf("probe-"+name, portA); err != nil {
		return err
	}
	if force {
		return r.post(portA, "torrents/setForceStart", "hashes="+hash, "value=true")
	}
	return nil
}

func (r *rig) hashOf(name string, port int) (string, error) {
	var hash string
	found, err := poll(30, func() (bool, error) {
		rows, err := r.info(port, "")
		hash = rows[name].Hash
		return hash != "", err
	})
	if err == nil && !found {
		err = fmt.Errorf("%s not added on port %d", name, port)
	}
	return hash, err
}

// post sends a request whose failure the run logs and survives.
func (r *rig) post(port int, action string, fields ...string) error {
	code, body, err := r.call(port, false, action, fields...)
	if err == nil && code != 200 && code != 204 {
		fmt.Printf("  %s %v on port %d answered %d: %.120s\n", action, fields, port, code, body)
	}
	return err
}

func (r *rig) resume(hash string, port int) error {
	if r.major >= 5 {
		return r.post(port, "torrents/start", "hashes="+hash)
	}
	return r.post(port, "torrents/resume", "hashes="+hash)
}

func (r *rig) pause(hash string, port int) error {
	if r.major >= 5 {
		return r.post(port, "torrents/stop", "hashes="+hash)
	}
	return r.post(port, "torrents/pause", "hashes="+hash)
}

func (r *rig) remove(port int, hashes ...string) error {
	return r.post(port, "torrents/delete", "hashes="+strings.Join(hashes, "|"), "deleteFiles=false")
}

// waitState waits for the seed to reach one of the states. A miss is logged,
// not fatal: a state that the run does not reach is absent from the capture.
func (r *rig) waitState(name string, tries int, states ...string) error {
	var last string
	ok, err := poll(tries, func() (bool, error) {
		rows, err := r.info(portA, "")
		last = rows["probe-"+name].State
		return slices.Contains(states, last), err
	})
	if err == nil && !ok {
		fmt.Printf("  probe-%s: wanted %v, last state %s\n", name, states, last)
	}
	return err
}

func (r *rig) throttle(cpus string) error {
	_, err := docker("update", "--cpus", cpus, r.a)
	return err
}

// snapshot sends the filter values in chunks, and returns the first listing.
// qBittorrent leaves out answers to a long run of parallel requests.
func (r *rig) snapshot(phase string) (map[string]row, error) {
	var first map[string]row
	for chunk := range slices.Chunk(filterValues, 8) {
		rows, err := r.snapshotOf(phase, chunk)
		if err != nil {
			return nil, err
		}
		if first == nil {
			first = rows
		}
	}
	return first, nil
}

// snapshotOf sends each of the values at once, between two listings, and
// tallies the torrents whose state is the same in both listings.
func (r *rig) snapshotOf(phase string, values []string) (map[string]row, error) {
	before, err := r.info(portA, "")
	if err != nil {
		return nil, err
	}
	out := filepath.Join(r.root, "b", "out")
	if err := os.RemoveAll(out); err != nil {
		return nil, err
	}
	if err := os.Mkdir(out, 0o755); err != nil {
		return nil, err
	}
	// curl keeps going after a failed request, so a missing file is a failed request.
	args := []string{"exec", r.b, "curl", "-sSf", "--max-time", "120", "-Z", "--parallel-immediate", "--parallel-max", "8"}
	for i, f := range values {
		args = append(args, "-o", fmt.Sprintf("/b/out/%d.json", i),
			fmt.Sprintf("http://127.0.0.1:%d/api/v2/torrents/info?filter=%s", portA, url.QueryEscape(f)))
	}
	_, _ = docker(args...)
	after, err := r.info(portA, "")
	if err != nil {
		return nil, err
	}
	for i, f := range values {
		body, err := os.ReadFile(filepath.Join(out, fmt.Sprintf("%d.json", i)))
		var rows map[string]row
		if err == nil {
			rows, err = rowsByName(body)
		}
		if err != nil {
			fmt.Printf("  %s: filter %s skipped: %v\n", phase, f, err)
			continue
		}
		for name, t := range before {
			if after[name].State != t.State {
				continue
			}
			got, returned := rows[name]
			if !returned {
				r.partial[f] = true
			}
			if returned && got.State != t.State {
				continue
			}
			if r.tallies[f] == nil {
				r.tallies[f] = map[string]*[2]bool{}
			}
			if r.tallies[f][t.State] == nil {
				r.tallies[f][t.State] = new([2]bool)
			}
			if returned {
				r.tallies[f][t.State][0] = true
			} else {
				r.tallies[f][t.State][1] = true
			}
		}
	}
	var seen []string
	for name, t := range before {
		seen = append(seen, strings.TrimPrefix(name, "probe-")+"="+t.State)
	}
	slices.Sort(seen)
	fmt.Printf("  %s %s %s: %s\n", time.Now().Format(time.TimeOnly), r.want, phase, strings.Join(seen, ", "))
	return before, nil
}

// cycle runs trigger once for each chunk of the filter values, and takes a
// snapshot of the chunk as soon as the seed is in the state. A snapshot of
// all chunks takes longer than a check of 100 MiB at 2% CPU.
// With name "", any torrent in the state counts.
func (r *rig) cycle(phase, name, state string, trigger func() error) error {
	in := func(rows map[string]row) bool {
		for n, t := range rows {
			if (name == "" || n == "probe-"+name) && t.State == state {
				return true
			}
		}
		return false
	}
	got := 0
	for chunk := range slices.Chunk(filterValues, 8) {
		if err := trigger(); err != nil {
			return err
		}
		for range 100 {
			rows, err := r.info(portA, "")
			if err != nil {
				return err
			}
			if in(rows) {
				if _, err := r.snapshotOf(phase, chunk); err != nil {
					return err
				}
				got++
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		for range 120 {
			rows, err := r.info(portA, "")
			if err != nil || !in(rows) {
				break
			}
			time.Sleep(time.Second)
		}
	}
	fmt.Printf("  %s: saw %s in %s for %d of %d chunks\n", phase, cmp.Or(name, "a torrent"), state, got,
		(len(filterValues)+7)/8)
	return nil
}

// catch takes snapshots until two of them saw the seed in the state.
func (r *rig) catch(phase, name, state string, tries int) error {
	got := 0
	for range tries {
		rows, err := r.snapshot(phase)
		if err != nil {
			return err
		}
		if rows["probe-"+name].State == state {
			if got++; got == 2 {
				return nil
			}
		}
	}
	fmt.Printf("  %s: saw probe-%s in %s %d times\n", phase, name, state, got)
	return nil
}

type categoryCase struct {
	Value         string          `json:"value"`
	Subcategories bool            `json:"subcategories"`
	Torrents      []categoryProbe `json:"torrents"`
}

type categoryProbe struct {
	Category string `json:"category"`
	Returned bool   `json:"returned"`
}

type tagCase struct {
	Value         string     `json:"value"`
	Subcategories bool       `json:"subcategories"`
	Torrents      []tagProbe `json:"torrents"`
}

type tagProbe struct {
	Tags     string `json:"tags"`
	Returned bool   `json:"returned"`
}

func (r *rig) categoriesAndTags() ([]categoryCase, []tagCase, error) {
	probes := map[string]struct{ category, tags string }{
		"c-movies": {"movies", "probe"}, "c-hd": {"movies/hd", "probe-extra"}, "c-deep": {"movies/hd/x", "other,probe"},
		"c-sibling": {"movies-extra", ""}, "c-none": {"", "other"},
	}
	for _, c := range []string{"movies", "movies/hd", "movies/hd/x", "movies-extra"} {
		if _, err := r.api(portA, "torrents/createCategory", "category="+c, "savePath="); err != nil {
			return nil, nil, err
		}
	}
	var hashes []string
	for _, name := range slices.Sorted(maps.Keys(probes)) {
		if err := r.place(name, filepath.Join(r.root, "data")); err != nil {
			return nil, nil, err
		}
		p := probes[name]
		var extra []string
		if p.category != "" {
			extra = append(extra, "category="+p.category)
		}
		hash, err := r.add(name, portA, "/data", false, true, extra...)
		if err != nil {
			return nil, nil, err
		}
		if p.tags != "" {
			if _, err := r.api(portA, "torrents/addTags", "hashes="+hash, "tags="+p.tags); err != nil {
				return nil, nil, err
			}
		}
		hashes = append(hashes, hash)
	}

	body, err := r.api(portA, "app/preferences")
	if err != nil {
		return nil, nil, err
	}
	var prefs map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &prefs); err != nil {
		return nil, nil, err
	}
	_, hasPref := prefs["use_subcategories"]
	settings := []bool{false, true}
	if !hasPref && version.Compare("go"+r.want, "go5.2.0") >= 0 {
		// These releases have no setting and always use subcategories.
		settings = []bool{true}
	}
	var categories []categoryCase
	var tags []tagCase
	for _, on := range settings {
		switch {
		case hasPref:
			if _, err := r.api(portA, "app/setPreferences", fmt.Sprintf(`json={"use_subcategories":%t}`, on)); err != nil {
				return nil, nil, err
			}
		case len(settings) == 2:
			// The Web API of these releases cannot set subcategories.
			if err := r.stopA(); err != nil {
				return nil, nil, err
			}
			if err := r.setConf(`Session\SubcategoriesEnabled`, strconv.FormatBool(on)); err != nil {
				return nil, nil, err
			}
			if err := r.start(r.a, portA); err != nil {
				return nil, nil, err
			}
		}
		time.Sleep(2 * time.Second)
		for _, value := range []string{"movies", "movies/hd", "movies-extra", "", "not_a_category"} {
			rows, err := r.info(portA, "?category="+url.QueryEscape(value))
			if err != nil {
				return nil, nil, err
			}
			c := categoryCase{Value: value, Subcategories: on}
			for name, p := range probes {
				_, returned := rows["probe-"+name]
				c.Torrents = append(c.Torrents, categoryProbe{p.category, returned})
			}
			slices.SortFunc(c.Torrents, func(a, b categoryProbe) int { return strings.Compare(a.Category, b.Category) })
			if value == "movies" {
				_, sub := rows["probe-c-hd"]
				if sub != on {
					return nil, nil, fmt.Errorf("subcategories set to %t, but category=movies returned movies/hd: %t", on, sub)
				}
			}
			categories = append(categories, c)
		}
		for _, value := range []string{"probe", ""} {
			rows, err := r.info(portA, "?tag="+url.QueryEscape(value))
			if err != nil {
				return nil, nil, err
			}
			all, err := r.info(portA, "")
			if err != nil {
				return nil, nil, err
			}
			c := tagCase{Value: value, Subcategories: on}
			for name := range probes {
				_, returned := rows["probe-"+name]
				c.Torrents = append(c.Torrents, tagProbe{all["probe-"+name].Tags, returned})
			}
			slices.SortFunc(c.Torrents, func(a, b tagProbe) int { return strings.Compare(a.Tags, b.Tags) })
			tags = append(tags, c)
		}
		fmt.Printf("  %s categories and tags with subcategories %t\n", r.want, on)
	}
	return categories, tags, r.remove(portA, hashes...)
}

func (r *rig) steady() error {
	data, datab := filepath.Join(r.root, "data"), filepath.Join(r.root, "datab")
	for _, n := range []string{"s-stalled", "s-stopped", "s-forced", "b-up"} {
		if err := r.place(n, data); err != nil {
			return err
		}
	}
	if err := r.place("b-dl", datab); err != nil {
		return err
	}
	if _, err := r.add("s-stalled", portA, "/data", true, true); err != nil {
		return err
	}
	// On 5.3.0 an add with stopped and skip_checking gives stoppedDL, so start
	// the torrent and then stop it to get stoppedUP.
	hst, err := r.add("s-stopped", portA, "/data", true, true)
	if err != nil {
		return err
	}
	hf, err := r.add("s-forced", portA, "/data", false, true)
	if err != nil {
		return err
	}
	if err := r.post(portA, "torrents/setForceStart", "hashes="+hf, "value=true"); err != nil {
		return err
	}
	if _, err := r.add("l-stopped", portA, "/data", false, false); err != nil {
		return err
	}
	if _, err := r.add("l-stalled", portA, "/data", true, false); err != nil {
		return err
	}
	hlf, err := r.add("l-forced", portA, "/data", false, false)
	if err != nil {
		return err
	}
	if err := r.post(portA, "torrents/setForceStart", "hashes="+hlf, "value=true"); err != nil {
		return err
	}
	if err := r.addMagnet("magnet", "c0ffee"+strings.Repeat("0", 34), false); err != nil {
		return err
	}
	if err := r.addMagnet("magnet-forced", "c0ffee"+strings.Repeat("1", 34), true); err != nil {
		return err
	}
	if err := r.waitState("s-stopped", 60, "stalledUP"); err != nil {
		return err
	}
	if err := r.pause(hst, portA); err != nil {
		return err
	}
	if err := r.waitState("s-stopped", 60, "stoppedUP", "pausedUP"); err != nil {
		return err
	}

	// A seeds b-up to B, and B seeds b-dl to A, both slowly.
	hup, err := r.add("b-up", portA, "/data", true, true)
	if err != nil {
		return err
	}
	hupB, err := r.add("b-up", portB, "/datab", false, false)
	if err != nil {
		return err
	}
	hdlB, err := r.add("b-dl", portB, "/datab", true, true)
	if err != nil {
		return err
	}
	hdl, err := r.add("b-dl", portA, "/data", false, false)
	if err != nil {
		return err
	}
	for _, s := range []struct {
		hash string
		port int
	}{{hupB, portB}, {hdl, portA}} {
		if err := r.post(s.port, "torrents/setDownloadLimit", "hashes="+s.hash, "limit="+strconv.Itoa(200<<10)); err != nil {
			return err
		}
		if err := r.resume(s.hash, s.port); err != nil {
			return err
		}
	}
	time.Sleep(3 * time.Second)
	if err := r.post(portB, "torrents/addPeers", "hashes="+hupB, "peers=127.0.0.1:6881"); err != nil {
		return err
	}
	if err := r.post(portA, "torrents/addPeers", "hashes="+hdl, "peers=127.0.0.1:6882"); err != nil {
		return err
	}
	if err := r.waitState("b-up", 120, "uploading"); err != nil {
		return err
	}
	if err := r.waitState("b-dl", 120, "downloading"); err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	for _, phase := range []string{"steady", "steady again"} {
		if _, err := r.snapshot(phase); err != nil {
			return err
		}
	}
	if err := r.remove(portA, hup, hdl); err != nil {
		return err
	}
	return r.remove(portB, hupB, hdlB)
}

func (r *rig) queued() error {
	if _, err := r.api(portA, "app/setPreferences", `json={"queueing_enabled":true,"max_active_downloads":1,`+
		`"max_active_uploads":1,"max_active_torrents":2,"dont_count_slow_torrents":false}`); err != nil {
		return err
	}
	var hashes []string
	for _, n := range []string{"s-q1", "s-q2", "s-q3", "l-q1", "l-q2", "l-q3"} {
		skip := strings.HasPrefix(n, "s-")
		if skip {
			if err := r.place(n, filepath.Join(r.root, "data")); err != nil {
				return err
			}
		}
		h, err := r.add(n, portA, "/data", true, skip)
		if err != nil {
			return err
		}
		hashes = append(hashes, h)
	}
	time.Sleep(8 * time.Second)
	if _, err := r.snapshot("queued"); err != nil {
		return err
	}
	if _, err := r.api(portA, "app/setPreferences", `json={"queueing_enabled":false}`); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	return r.remove(portA, hashes...)
}

func (r *rig) checking() error {
	data := filepath.Join(r.root, "data")
	if err := r.place("b-check", data); err != nil {
		return err
	}
	hc, err := r.add("b-check", portA, "/data", true, true)
	if err != nil {
		return err
	}
	if err := r.waitState("b-check", 60, "stalledUP"); err != nil {
		return err
	}
	recheck := func(hash string) func() error {
		return func() error { return r.post(portA, "torrents/recheck", "hashes="+hash) }
	}
	if err := r.throttle("0.02"); err != nil {
		return err
	}
	if err := r.cycle("checkingUP", "b-check", "checkingUP", recheck(hc)); err != nil {
		return err
	}
	if err := r.waitState("b-check", 120, "stalledUP"); err != nil {
		return err
	}
	// A recheck of a stopped torrent that has all its data.
	if err := r.pause(hc, portA); err != nil {
		return err
	}
	if err := r.waitState("b-check", 60, "pausedUP", "stoppedUP"); err != nil {
		return err
	}
	if err := r.cycle("recheck stopped", "b-check", "checkingUP", recheck(hc)); err != nil {
		return err
	}
	if err := r.throttle("0"); err != nil {
		return err
	}
	if err := r.waitState("b-check", 120, "pausedUP", "stoppedUP"); err != nil {
		return err
	}
	if _, err := r.snapshot("recheck stopped done"); err != nil {
		return err
	}
	if err := r.remove(portA, hc); err != nil {
		return err
	}

	// The second half of the data is wrong, so the torrent stays incomplete,
	// and each recheck shows checkingDL.
	if err := r.place("b-checkdl", data); err != nil {
		return err
	}
	size := seeds["b-checkdl"].size
	f, err := os.OpenFile(filepath.Join(data, "probe-b-checkdl"), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, err = f.WriteAt(make([]byte, size/2), size/2)
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	hcd, err := r.add("b-checkdl", portA, "/data", true, false)
	if err != nil {
		return err
	}
	if err := r.waitState("b-checkdl", 120, "stalledDL"); err != nil {
		return err
	}
	if err := r.throttle("0.02"); err != nil {
		return err
	}
	if err := r.cycle("checkingDL", "b-checkdl", "checkingDL", recheck(hcd)); err != nil {
		return err
	}
	if err := r.throttle("0"); err != nil {
		return err
	}
	if err := r.waitState("b-checkdl", 120, "stalledDL"); err != nil {
		return err
	}
	return r.remove(portA, hcd)
}

func (r *rig) moving() error {
	if err := r.place("b-move", filepath.Join(r.root, "data")); err != nil {
		return err
	}
	hm, err := r.add("b-move", portA, "/data", true, true)
	if err != nil {
		return err
	}
	if err := r.waitState("b-move", 60, "stalledUP"); err != nil {
		return err
	}
	if err := r.throttle("0.01"); err != nil {
		return err
	}
	if err := r.post(portA, "torrents/setLocation", "hashes="+hm, "location=/move"); err != nil {
		return err
	}
	if err := r.catch("moving", "b-move", "moving", 30); err != nil {
		return err
	}
	if err := r.throttle("0"); err != nil {
		return err
	}
	if err := r.waitState("b-move", 120, "stalledUP"); err != nil {
		return err
	}
	return r.remove(portA, hm)
}

// faults gets missingFiles, error, and checkingResumeData. Data that goes
// away while qBittorrent is down gives missingFiles at the next start; a
// delete and a recheck gives stalledDL instead. An unreadable file gives error
// when a peer asks for it, or on 5.3.0 at a recheck.
func (r *rig) faults() error {
	data := filepath.Join(r.root, "data")
	names := []string{"s-missing", "s-missing-stopped", "s-error", "s-unreadable"}
	hashes := map[string]string{}
	for _, n := range names {
		if err := r.place(n, data); err != nil {
			return err
		}
		h, err := r.add(n, portA, "/data", true, true)
		if err != nil {
			return err
		}
		hashes[n] = h
	}
	for _, n := range names {
		if err := r.waitState(n, 60, "stalledUP"); err != nil {
			return err
		}
	}
	if err := r.pause(hashes["s-missing-stopped"], portA); err != nil {
		return err
	}
	if err := r.waitState("s-missing-stopped", 60, "pausedUP", "stoppedUP"); err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	if err := r.stopA(); err != nil {
		return err
	}
	for _, n := range []string{"s-missing", "s-missing-stopped"} {
		if err := os.Remove(filepath.Join(data, "probe-"+n)); err != nil {
			return err
		}
	}
	for _, n := range []string{"s-error", "s-unreadable"} {
		path := filepath.Join(data, "probe-"+n)
		if err := os.Chmod(path, 0); err != nil {
			return err
		}
		onCleanup(func() { _ = os.Chmod(path, 0o644) })
	}
	// checkingResumeData lasts only while the torrents load.
	if err := r.throttle("0.01"); err != nil {
		return err
	}
	started := false
	if err := r.cycle("start", "", "checkingResumeData", func() error {
		if started {
			if err := r.stopA(); err != nil {
				return err
			}
		}
		started = true
		return r.start(r.a, portA)
	}); err != nil {
		return err
	}
	if err := r.throttle("0"); err != nil {
		return err
	}
	if err := r.waitState("s-missing", 60, "missingFiles"); err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	if _, err := r.snapshot("after start"); err != nil {
		return err
	}

	// A peer asks for the unreadable data.
	hb, err := r.add("s-error", portB, "/datab", true, false)
	if err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	if err := r.post(portB, "torrents/addPeers", "hashes="+hb, "peers=127.0.0.1:6881"); err != nil {
		return err
	}
	if err := r.waitState("s-error", 40, "error"); err != nil {
		return err
	}
	// A recheck of an unreadable file.
	if err := r.post(portA, "torrents/recheck", "hashes="+hashes["s-unreadable"]); err != nil {
		return err
	}
	if err := r.waitState("s-unreadable", 15, "error"); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	if _, err := r.snapshot("error"); err != nil {
		return err
	}
	// A stop of a torrent in error or missingFiles.
	for _, n := range []string{"s-error", "s-unreadable", "s-missing"} {
		if err := r.pause(hashes[n], portA); err != nil {
			return err
		}
	}
	time.Sleep(5 * time.Second)
	_, err = r.snapshot("stopped after a fault")
	return err
}

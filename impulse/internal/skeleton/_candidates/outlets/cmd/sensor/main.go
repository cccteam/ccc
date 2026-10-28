// Package main is the sensor: the program the release ships to the machines that report
// readings, which the machines outlet serves to its installed copies and which keeps
// itself at the outlet's release. A run does one thing against the outlet, posts one
// reading or lists the latest, and then follows the release the outlet's answers name:
// when the server is a newer release than this copy, it fetches that release's file
// from the outlet with the token it already carries, verifies the checksum, swaps the
// file in beside itself and exits for its service manager to start the new release.
//
//	sensor post <value>    post one reading of the value, taken now
//	sensor list            list the latest readings, newest first
//
// The settings are the machine's environment:
//
//	SENSOR_OUTLET  the machines outlet's URL as the router mounts it, https://app.example.com/machines
//	SENSOR_TOKEN   an application token a staff login issued for the outlet on the console's Tokens page
//	SENSOR_TENANT  the tenant the readings belong to; north in development
//	SENSOR_SOURCE  the name the readings carry for this machine; the hostname when unset
//
// A person installs the first copy on a machine from the release's assets or with
// go install <module>/cmd/sensor@<release>. The image build stamps the release into
// every copy it compiles (resource/shipped.Release); a copy built from a checkout is a
// development copy and never upgrades.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/cccteam/ccc/resource/shipped"
	"github.com/go-playground/errors/v5"
	"github.com/sethvargo/go-envconfig"
)

// programName is the name the outlet serves the program under: the base of its
// directory, as ShipsProgram("cmd/sensor") declares it.
const programName = "sensor"

// The commands.
const (
	commandPost = "post"
	commandList = "list"
)

// The exit codes: the arguments or the settings refused before anything is sent (as the
// flag package exits), and a run that failed.
const (
	exitRefused = 2
	exitFailed  = 1
)

// usage is what -h prints, and what a refusal of the arguments is followed by.
const usage = `usage:
  sensor post <value>    post one reading of the value, taken now
  sensor list            list the latest readings, newest first
`

// errRefused marks arguments the flag package refused and reported itself, and errHelp
// marks -h, answered with the usage: the run then has nothing to do.
var (
	errRefused = errors.New("the arguments were refused")
	errHelp    = errors.New("the usage was asked for")
)

// settings is what the sensor reads from its environment.
type settings struct {
	Outlet string `env:"SENSOR_OUTLET,required"`
	Token  string `env:"SENSOR_TOKEN,required"`
	Tenant string `env:"SENSOR_TENANT,required"`
	Source string `env:"SENSOR_SOURCE"`
}

// command is one run's work: the word and, for a post, the value.
type command struct {
	word  string
	value float64
}

// newReading is what the sensor posts: the create properties of a reading, as the
// outlet's consolidated handler takes them under the tenant's readings.
type newReading struct {
	Source     string    `json:"source"`
	Value      float64   `json:"value"`
	RecordedAt time.Time `json:"recordedAt"`
}

// operation is one entry of the consolidated patch the outlet takes at PATCH
// <outlet>/resources: an add under the tenant's readings creates one reading.
type operation struct {
	Op    string     `json:"op"`
	Path  string     `json:"path"`
	Value newReading `json:"value"`
}

// reading is one reading as the outlet lists it.
type reading struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Value      float64   `json:"value"`
	RecordedAt time.Time `json:"recordedAt"`
}

func main() {
	os.Exit(run())
}

// run executes the command line under a context an interrupt cancels and answers the
// exit code.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

// execute runs the command with its arguments and answers the exit code: the work
// first, then the upgrade when the outlet's answers named a newer release. A refusal of
// the arguments and a failed run say why on stderr.
func execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	cmd, err := parse(args, stderr)
	switch {
	case errors.Is(err, errHelp):
		return 0
	case errors.Is(err, errRefused):
		return exitRefused
	case err != nil:
		fmt.Fprintln(stderr, err)
		fmt.Fprint(stderr, usage)

		return exitRefused
	}
	s, err := readSettings(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)

		return exitRefused
	}

	// Every call goes through the follower's transport, which reads the server's release
	// from each answer, and carries the token; the follower fetches a new release through
	// the same client, so the fetch carries the token too.
	follower := &shipped.Follower{Outlet: s.Outlet, Name: programName}
	c := &client{
		outlet: s.Outlet,
		tenant: s.Tenant,
		http:   &http.Client{Transport: follower.Transport(bearer{token: s.Token, next: http.DefaultTransport})},
	}
	follower.Client = c.http

	if err := cmd.run(ctx, c, s.Source, stdout); err != nil {
		fmt.Fprintln(stderr, err)

		return exitFailed
	}

	return follow(ctx, follower, stdout, stderr)
}

// parse reads the command from the arguments: post with its value, or list. The flag
// set answers -h with the usage and refuses a flag, since the sensor takes none.
func parse(args []string, stderr io.Writer) (*command, error) {
	fs := flag.NewFlagSet(programName, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
	}
	// The flag package has said what it refused, or printed the usage: the run learns
	// which, and nothing of the arguments comes back with it.
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errHelp
		}

		return nil, errRefused
	}
	words := fs.Args()
	if len(words) == 0 {
		return nil, errors.New("a command is required: post or list")
	}
	switch word := words[0]; word {
	case commandPost:
		if len(words) != 2 {
			return nil, errors.New("post takes one argument, the value of the reading")
		}
		value, err := strconv.ParseFloat(words[1], 64)
		if err != nil {
			return nil, errors.Newf("%q is not a value: a reading is a number, such as 21.5", words[1])
		}

		return &command{word: word, value: value}, nil
	case commandList:
		if len(words) != 1 {
			return nil, errors.New("list takes no argument")
		}

		return &command{word: word}, nil
	default:
		return nil, errors.Newf("unknown command %q: post or list", word)
	}
}

// readSettings reads the environment, the hostname standing in for an unset source.
func readSettings(ctx context.Context) (*settings, error) {
	s := &settings{}
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: s, Lookuper: envconfig.OsLookuper()}); err != nil {
		return nil, errors.Wrap(err, "envconfig.ProcessWith()")
	}
	if s.Source == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, errors.Wrap(err, "os.Hostname()")
		}
		s.Source = hostname
	}

	return s, nil
}

// run does the command's work and prints the result.
func (c *command) run(ctx context.Context, cl *client, source string, out io.Writer) error {
	switch c.word {
	case commandPost:
		posted := newReading{Source: source, Value: c.value, RecordedAt: time.Now().UTC().Truncate(time.Second)}
		id, err := cl.post(ctx, posted)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Posted reading %s: %s %g at %s\n", id, posted.Source, posted.Value, posted.RecordedAt.Format(time.RFC3339))
	case commandList:
		readings, err := cl.list(ctx)
		if err != nil {
			return err
		}
		printReadings(out, readings)
	}

	return nil
}

// printReadings writes the readings as a table, one per line, newest first as the outlet
// lists them.
func printReadings(out io.Writer, readings []reading) {
	if len(readings) == 0 {
		fmt.Fprintln(out, "No readings")

		return
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "RECORDED AT\tSOURCE\tVALUE\tID")
	for _, r := range readings {
		fmt.Fprintf(w, "%s\t%s\t%g\t%s\n", r.RecordedAt.Format(time.RFC3339), r.Source, r.Value, r.ID)
	}
	// A tabwriter writing to a writer that never fails has nothing to report.
	_ = w.Flush()
}

// follow upgrades the copy when the outlet's answers named a release newer than this
// copy's: the new file is in place beside the running one, so the run says so and exits
// 0 for the service manager to start it. A failed upgrade is reported, and the run's
// work stands: the copy stays at its release and tries again on its next run.
func follow(ctx context.Context, f *shipped.Follower, stdout, stderr io.Writer) int {
	release, ok := f.Newer()
	if !ok {
		return 0
	}
	upgraded, err := f.Upgrade(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "The outlet is release %s and this copy is %s; upgrading failed: %v\n", release, shipped.OwnRelease(), err)

		return exitFailed
	}
	fmt.Fprintf(stdout, "Upgraded from release %s to %s; exiting for the service manager to start it\n", shipped.OwnRelease(), upgraded)

	return 0
}

// client calls the machines outlet for one tenant.
type client struct {
	outlet string
	tenant string
	http   *http.Client
}

// post creates one reading through the outlet's consolidated handler and returns its id.
func (c *client) post(ctx context.Context, r newReading) (string, error) {
	body, err := postBody(c.tenant, r)
	if err != nil {
		return "", err
	}
	// The consolidated handler answers the ids it created by resource.
	var created map[string][]string
	if err := c.call(ctx, http.MethodPatch, c.outlet+"/resources", body, &created); err != nil {
		return "", err
	}
	ids := created["readings"]
	if len(ids) != 1 {
		return "", errors.Newf("the outlet answered %v, want one reading created", created)
	}

	return ids[0], nil
}

// list fetches the latest readings of the tenant, newest first, one page.
func (c *client) list(ctx context.Context) ([]reading, error) {
	var readings []reading
	if err := c.call(ctx, http.MethodGet, c.outlet+readingsPath(c.tenant), nil, &readings); err != nil {
		return nil, err
	}

	return readings, nil
}

// call sends one request and decodes the answer into result; an answer other than 200
// is an error carrying the outlet's sentence.
func (c *client) call(ctx context.Context, method, url string, body []byte, result any) error {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "http.NewRequestWithContext()")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.Wrapf(err, "%s %s", method, url)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

		return errors.Newf("%s %s answered %d: %s", method, url, resp.StatusCode, bytes.TrimSpace(answer))
	}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return errors.Wrapf(err, "decoding the answer of %s %s", method, url)
	}

	return nil
}

// postBody is the consolidated patch that creates one reading under the tenant.
func postBody(tenant string, r newReading) ([]byte, error) {
	body, err := json.Marshal([]operation{{Op: "add", Path: readingsPath(tenant), Value: r}})
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return body, nil
}

// readingsPath is the tenant's readings under the outlet's prefix.
func readingsPath(tenant string) string {
	return "/tenants/" + tenant + "/readings"
}

// bearer adds the token to every request.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	resp, err := b.next.RoundTrip(r)
	if err != nil {
		return nil, errors.Wrap(err, "RoundTrip()")
	}

	return resp, nil
}

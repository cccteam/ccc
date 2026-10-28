// Package main is the droid: the program the release ships to the droids that report
// ship telemetry, which the droids outlet serves to its installed copies and which keeps
// itself at the outlet's release. A run sends one telemetry frame, a reading of one
// subsystem of the ship the droid rides, through the outlet's ingest method
// (IngestDroidReports), and then follows the release the outlet's answers name: when
// the server is a newer release than this copy, it fetches that release's file from the
// outlet with the token it already carries, verifies the checksum, swaps the file in
// beside itself and exits for its service manager to start the new release.
//
//	droid <subsystem> <reading>    send one reading of the subsystem, taken now
//
// The settings are the droid's environment:
//
//	DROID_OUTLET    the droids outlet's URL as the router mounts it, https://lodestar.example.com/droids
//	DROID_TOKEN     the droid account's token: the key the server seeds as its application token (APP_DROIDS_API_KEY), or a token a crew member issued
//	DROID_SECTOR    the sector the ship is in: anvil
//	DROID_SHIP      the ship the droid rides, by id: 70000000-0000-4000-8000-000000000003 is the Lantern
//	DROID_FIRMWARE  the firmware the frame names; 7.2 when unset
//
// The frame is the firmware's: the droid writes it as JSON of its own shape, which the
// service stores as it came (pkg/telemetry, Frame) and the droid channel's list carries
// back. A person installs the first copy on a droid from the release's assets or with
// go install github.com/cccteam/ccc/resource/lodestar/cmd/droid@<release>. The image
// build stamps the release into every copy it compiles (resource/shipped.Release); a
// copy built from a checkout is a development copy and never upgrades.
//
// Demonstrates: outlet.programs.
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
	"time"

	"github.com/cccteam/ccc/resource/shipped"
	"github.com/go-playground/errors/v5"
	"github.com/sethvargo/go-envconfig"
)

// programName is the name the outlet serves the program under: the base of its
// directory, as ShipsProgram("cmd/droid") declares it.
const programName = "droid"

// The exit codes: the arguments or the settings refused before anything is sent (as the
// flag package exits), and a run that failed.
const (
	exitRefused = 2
	exitFailed  = 1
)

// usage is what -h prints, and what a refusal of the arguments is followed by.
const usage = `usage:
  droid <subsystem> <reading>    send one reading of the subsystem, taken now
`

// defaultFirmware is the firmware the frame names when the environment names none.
const defaultFirmware = "7.2"

// errRefused marks arguments the flag package refused and reported itself, and errHelp
// marks -h, answered with the usage: the run then has nothing to do.
var (
	errRefused = errors.New("the arguments were refused")
	errHelp    = errors.New("the usage was asked for")
)

// settings is what the droid reads from its environment.
type settings struct {
	Outlet   string `env:"DROID_OUTLET,required"`
	Token    string `env:"DROID_TOKEN,required"`
	Sector   string `env:"DROID_SECTOR,required"`
	Ship     string `env:"DROID_SHIP,required"`
	Firmware string `env:"DROID_FIRMWARE"`
}

// observation is one run's work: the subsystem and the reading taken.
type observation struct {
	subsystem string
	reading   float64
}

// report is the ingest method's payload, flat, one reading per call, with the firmware's
// raw frame behind the reading.
type report struct {
	ShipID     string          `json:"shipId"`
	Subsystem  string          `json:"subsystem"`
	Reading    float64         `json:"reading"`
	RecordedAt time.Time       `json:"recordedAt"`
	Frame      json.RawMessage `json:"frame"`
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

// execute sends the reading and answers the exit code: the work first, then the upgrade
// when the outlet's answers named a newer release. A refusal of the arguments and a
// failed run say why on stderr.
func execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	o, err := parse(args, stderr)
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
	client := &http.Client{Transport: follower.Transport(bearer{token: s.Token, next: http.DefaultTransport})}
	follower.Client = client

	r, err := newReport(s, o, time.Now().UTC().Truncate(time.Second))
	if err != nil {
		fmt.Fprintln(stderr, err)

		return exitFailed
	}
	if err := send(ctx, client, s.Outlet+ingestPath(s.Sector), r); err != nil {
		fmt.Fprintln(stderr, err)

		return exitFailed
	}
	fmt.Fprintf(stdout, "Sent %s %g for ship %s in %s at %s\n", r.Subsystem, r.Reading, r.ShipID, s.Sector, r.RecordedAt.Format(time.RFC3339))

	return follow(ctx, follower, stdout, stderr)
}

// parse reads the observation from the arguments: the subsystem and the reading. The
// flag set answers -h with the usage and refuses a flag, since the droid takes none.
func parse(args []string, stderr io.Writer) (*observation, error) {
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
	if len(words) != 2 {
		return nil, errors.New("two arguments are required: the subsystem and the reading")
	}
	if words[0] == "" {
		return nil, errors.New("the subsystem is required")
	}
	reading, err := strconv.ParseFloat(words[1], 64)
	if err != nil {
		return nil, errors.Newf("%q is not a reading: a reading is a number, such as 0.55", words[1])
	}

	return &observation{subsystem: words[0], reading: reading}, nil
}

// readSettings reads the environment, the default firmware standing in for an unset one.
func readSettings(ctx context.Context) (*settings, error) {
	s := &settings{}
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: s, Lookuper: envconfig.OsLookuper()}); err != nil {
		return nil, errors.Wrap(err, "envconfig.ProcessWith()")
	}
	if s.Firmware == "" {
		s.Firmware = defaultFirmware
	}

	return s, nil
}

// newReport shapes the ingest payload for the observation: the ship and the reading, and
// the frame as the firmware writes it, naming the firmware and the raw reading under
// the subsystem's key.
func newReport(s *settings, o *observation, at time.Time) (*report, error) {
	frame, err := json.Marshal(map[string]any{
		"fw":        s.Firmware,
		o.subsystem: map[string]any{"raw": o.reading, "takenAt": at.Format(time.RFC3339)},
	})
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return &report{ShipID: s.Ship, Subsystem: o.subsystem, Reading: o.reading, RecordedAt: at, Frame: frame}, nil
}

// ingestPath is the ingest method under the outlet's prefix, in the sector.
func ingestPath(sector string) string {
	return "/sectors/" + sector + "/ingest-droid-reports"
}

// send posts the report; an answer other than 200 is an error carrying the outlet's
// sentence.
func send(ctx context.Context, client *http.Client, url string, r *report) error {
	body, err := json.Marshal(r)
	if err != nil {
		return errors.Wrap(err, "json.Marshal()")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "http.NewRequestWithContext()")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return errors.Wrapf(err, "POST %s", url)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

		return errors.Newf("POST %s answered %d: %s", url, resp.StatusCode, bytes.TrimSpace(answer))
	}

	return nil
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

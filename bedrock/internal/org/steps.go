// steps.go is what the Cloud Build triggers 0-bootstrap renders run (cloud-build.tf):
// the images, the scripts of the two steps that put the pinned bedrock in place, and the
// timeouts. The steps are the trigger's, so a pull request cannot change them; what they
// run is read at run time, off placement.json in the checkout, so the trigger's steps
// never hold a bedrock version: a trigger's steps lag one apply behind the repository,
// and a pull request that moves the pin already runs the new bedrock under the old
// steps. The images and the install environment are the application pipeline's
// (internal/render), restated here; a test holds the two together.

package org

import "strings"

// The images the steps run in: gcloud's (bash, curl and python3), the Go image a commit
// pin is built in, pinned at the digest the application pipeline builds with, and the
// OpenTofu image the check and the commands run in.
const (
	gcloudImage   = "gcr.io/cloud-builders/gcloud"
	goImage       = "cgr.dev/chainguard/go@sha256:2c20827746d752c1f18989847956637a0dc979c42507f4059e1d9559ee7733af"
	openTofuImage = "ghcr.io/opentofu/opentofu:1.12.6"
)

// goInstallEnv is the environment go install builds a commit pin in, the application
// pipeline's: a static binary the OpenTofu image can run, the image's Go and no other,
// the caches under /tmp, the module proxy, and Go's checksum database verifying the
// module whatever the image would set.
var goInstallEnv = []string{
	"CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOFLAGS=-trimpath",
	"GOPATH=/tmp/go", "GOCACHE=/tmp/go-build", "GOMODCACHE=/tmp/go-mod",
	"GOPROXY=https://proxy.golang.org", "GOSUMDB=sum.golang.org", "GONOSUMDB=", "GOPRIVATE=", "GOINSECURE=",
}

// Where the steps leave the pinned bedrock: the home directory Cloud Build shares between
// a build's steps, the binary in it, and the note the fetch step leaves for the build
// step when the pin is a commit.
const (
	bedrockHome   = "/builder/home"
	bedrockPath   = bedrockHome + "/bedrock"
	commitPinNote = bedrockHome + "/bedrock-commit"
)

// The steps' timeouts in seconds: a release's download (three tries of a minute each,
// with room to spare), a commit pin's build, the check, a plan of every layer side by
// side, and the apply of every layer in order (1-org's projects and 2-env three times
// among them).
const (
	fetchSeconds = 300
	buildSeconds = 600
	checkSeconds = 600
	planSeconds  = 3600
	applySeconds = 7200
)

// fetchBedrockScript is the FetchBedrock step, run by python3 in the gcloud image with
// the home directory and the download host as its arguments. It reads the pin off
// placement.json in the checkout. A release: the linux/amd64 binary of the release is
// downloaded from GitHub and verified against bedrockSha256, as the application
// pipeline's first step does with curl and sha256sum. A commit pin: the version is left
// in a note for the next step, which builds it. No pin: the run stops here and says what
// records one. The script holds no $, which Cloud Build would read as a substitution.
const fetchBedrockScript = `import hashlib
import json
import os
import re
import sys
import time
import urllib.request

home, host = sys.argv[1], sys.argv[2]
with open("placement.json", encoding="utf-8") as f:
    placement = json.load(f)
version = placement.get("bedrockVersion", "")
digest = placement.get("bedrockSha256", "")
if not version:
    sys.exit("placement.json pins no bedrock (bedrockVersion): run bedrock upgrade in this repository, which records the pin and renders, and commit what it changed")
if re.search(r"[-.][0-9]{14}-[0-9a-f]{12}\Z", version):
    with open(home + "/bedrock-commit", "w", encoding="utf-8") as f:
        f.write(version)
    print("bedrock " + version + " is a commit pin: the next step builds it with go install")
    sys.exit(0)
if not re.fullmatch(r"[0-9a-f]{64}", digest):
    sys.exit("placement.json pins bedrock " + version + " without the checksum of its linux/amd64 binary (bedrockSha256): bedrock upgrade " + version + " records it from the release's checksums.txt")
url = host + "/cccteam/ccc/releases/download/bedrock/" + version + "/bedrock-linux-amd64"
data = None
for attempt in range(3):
    try:
        with urllib.request.urlopen(url, timeout=60) as response:
            data = response.read()
        break
    except OSError as err:
        print("attempt " + str(attempt + 1) + " of 3 to download " + url + " failed: " + str(err))
        time.sleep(10)
if data is None:
    sys.exit("could not download " + url)
if hashlib.sha256(data).hexdigest() != digest:
    sys.exit(url + " does not match bedrockSha256 in placement.json: the file downloaded is not the release's binary")
with open(home + "/bedrock", "wb") as f:
    f.write(data)
os.chmod(home + "/bedrock", 0o755)
os.execv(home + "/bedrock", [home + "/bedrock", "--version"])
`

// buildBedrockScript is the BuildBedrock step, run by sh in the Go image: it builds the
// commit pin the fetch step noted, with go install into the home directory, where Go's
// checksum database verifies the module; with a release pin it has nothing to do. The $
// is doubled for Cloud Build, which reads a single one as a substitution.
const buildBedrockScript = `set -eu
if [ ! -f ` + commitPinNote + ` ]; then
  echo "a release pin: nothing to build"
  exit 0
fi
go install -ldflags="-s -w" github.com/cccteam/ccc/bedrock@"$$(cat ` + commitPinNote + `)"
` + bedrockPath + ` --version
`

// stepScript indents a step's script for an HCL heredoc at the depth of a locals block.
func stepScript(script string) string {
	lines := strings.Split(strings.TrimRight(script, "\n"), "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = "    " + line
		}
	}

	return strings.Join(lines, "\n")
}

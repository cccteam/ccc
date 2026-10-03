# harbor's container image: one image that serves the site (/app, the entrypoint) and carries
# the deployment's migrate command (/migrate, which the pipeline takes out of it and runs on the
# build worker) and the job process (/jobs, the command its Cloud Run job is deployed with).
# Seeded by bedrock from the code's shape and then yours: bedrock render writes it once and
# never again, so the image build can grow with the application.
#
# Every base image is referenced by digest, never by tag alone: the build is reproducible,
# nothing updates underneath it, and a later security scan targets exactly what was deployed.
# To find a digest, pull the tag and inspect it:
#
#     docker pull cgr.dev/chainguard/go:latest
#     docker inspect cgr.dev/chainguard/go:latest --format='{{index .RepoDigests 0}}'
#
# Build arguments: VERSION (the release; the process reports it as APP_VERSION) and COMMIT
# (the commit it was built from, recorded on the image). A local build:
#
#     podman build --build-arg VERSION=dev --build-arg COMMIT=$(git rev-parse HEAD) -t harbor .
#
# Build secrets: a secret declared in infrastructure/terraform.tfvars (build_secrets; a
# component license for the browser build) reaches the build as a BuildKit secret, never
# as a build argument, which the image would keep. Mount it in the one step that needs it:
#
#     RUN --mount=type=secret,id=NAME,required=true NAME="$(cat /run/secrets/NAME)" bun run build
#
# A local build passes it the same way: podman build --secret id=NAME,src=<file> ...
#
# The pipeline's build is cloudbuild.yaml's BuildImage step. It builds the two dependency
# stages below first and exports their layers alone to the registry's cache, one tag per
# commit, then builds the rest fresh in every environment, reading those caches and
# exporting nothing: the compile and the bundles are never served from a cache, and no
# per-build file of the pipeline is an input of any layer, since every stage copies its
# inputs by name.

ARG VERSION=dev
ARG COMMIT=unknown
# The job of this build for the job process, as the Cloud Run API names it: the pipeline
# makes it from the stack's template job and passes its name here, and the image names it
# to the site as APP_JOBS_JOB, so each revision starts the job of its own build.
ARG JOBS_JOB=

# ---- Dependencies: the downloads, cached per commit --------------------------------------
# Two reserved stages. The image build exports each one's layers to the registry under
# cache-<commit>-go and cache-<commit>-web, for the environments after the first to read,
# so a stage holds its lockfile copy and its one command and nothing else: bedrock check
# refuses more (a WORKDIR line is fine, and so is the bun binary's copy). What they hold
# is pinned by go.sum and bun.lock and verified at download, so a stored copy equals a
# fresh one.
#
# The go image is pinned at a digest that carries a toolchain at least as new as go.mod
# declares (go1.27.1, published 2026-09-02), found the way the header says. The compile
# stage sets GOTOOLCHAIN=local, which makes a go.mod that outruns the image fail the build
# there instead of downloading a toolchain: the fix is a new digest here, never a download.
FROM cgr.dev/chainguard/go@sha256:694c79dc301a249df5f2541aff2d82718d4ef3ff36bfa8a9eaee55ecadd40d16 AS go-modules
WORKDIR /go/src/app
COPY go.mod go.sum ./
RUN go mod download

# bun installs each workspace from its bun.lock; Angular's CLI itself runs under Node, so the
# installer and the builders are a node image with bun's pinned binary copied in from bun's
# own image. Nothing else of that image is used. --frozen-lockfile installs exactly what
# bun.lock records or fails: the lockfile is the build's input, never its output.
FROM docker.io/oven/bun@sha256:296a79bbc988bb0a91ef11099af70a78a8cba98b73fd53f7b2a7715b7c86ced2 AS bun-binary

FROM docker.io/library/node@sha256:392e1e23f34da768d8d1f4e502b64f200d3be3465934d4b7930f57d7e2fc1989 AS web-packages
COPY --from=bun-binary /usr/local/bin/bun /usr/local/bin/bun
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
WORKDIR /src/portal
COPY portal/package.json portal/bun.lock ./
RUN bun install --frozen-lockfile

# ---- Go: the server, the migrate command and the job process, built fresh in every environment ----
# The compile reads the module cache from go-modules, at the go image's default place (the
# image runs as root with no GOPATH set, so $HOME/go), and the sources by name. GOPROXY=off:
# a module the cache lacks fails the compile rather than being downloaded, so the layer
# depends on nothing but the copied files and the downloaded modules. -mod=readonly keeps
# go.mod an input, never rewritten.
FROM cgr.dev/chainguard/go@sha256:694c79dc301a249df5f2541aff2d82718d4ef3ff36bfa8a9eaee55ecadd40d16 AS build-env

ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=local \
    GOPROXY=off \
    GOFLAGS=-mod=readonly

WORKDIR /go/src/app

COPY --from=go-modules /root/go/pkg/mod /root/go/pkg/mod
COPY go.mod go.sum ./
COPY *.go ./
# One line per top-level directory holding Go packages, test files aside, as the code had
# them when this file was seeded. A new top-level package directory gets its own line
# here; a missing one fails the compile within seconds, naming the package.
COPY cmd/ cmd/
COPY pkg/ pkg/

# -trimpath keeps the build directory out of the binaries; -s -w drop the symbol table and
# the debug information, which nothing in a deployed image reads. No build tags: the
# session library's skipAuth tag (Procfile) simulates the directory and never ships.
RUN go build -trimpath -ldflags="-s -w" -o /build/app . && \
    go build -trimpath -ldflags="-s -w" -o /build/migrate ./cmd/deployment/migrate && \
    go build -trimpath -ldflags="-s -w" -o /build/jobs ./cmd/jobs

# ---- Browser: the Angular bundles, built fresh in every environment ----------------------
# One stage per workspace.

FROM docker.io/library/node@sha256:392e1e23f34da768d8d1f4e502b64f200d3be3465934d4b7930f57d7e2fc1989 AS web-build-env

COPY --from=bun-binary /usr/local/bin/bun /usr/local/bin/bun

# VERSION reaches this stage as an environment variable of the build, so the workspace's
# build script can stamp the release into each bundle and the browser can send it with
# its requests; a local build without the argument stamps dev.
ARG VERSION
ENV CI=true

WORKDIR /src/web

# The installed packages come from web-packages, the source after them: a change to the
# source rebuilds the bundles and never the install.
COPY --from=web-packages /src/web/node_modules ./node_modules
COPY web/ ./

# `bun run build` builds every browser app of the workspace (web/package.json), whose
# default configuration is production; it writes dist/console.
RUN bun run build

FROM docker.io/library/node@sha256:392e1e23f34da768d8d1f4e502b64f200d3be3465934d4b7930f57d7e2fc1989 AS portal-build-env

COPY --from=bun-binary /usr/local/bin/bun /usr/local/bin/bun

# VERSION reaches this stage as an environment variable of the build, so the workspace's
# build script can stamp the release into each bundle and the browser can send it with
# its requests; a local build without the argument stamps dev.
ARG VERSION
ENV CI=true

WORKDIR /src/portal

# The installed packages come from web-packages, the source after them: a change to the
# source rebuilds the bundles and never the install.
COPY --from=web-packages /src/portal/node_modules ./node_modules
COPY portal/ ./

# `bun run build` builds every browser app of the workspace (portal/package.json), whose
# default configuration is production; it writes dist/portal.
RUN bun run build

# ---- Runtime: a static image with the three binaries, the bundles, and the schema -----------
FROM cgr.dev/chainguard/static@sha256:1fe35025bd06652914560fd01af12a705c645c5566a8d40766b2a4769f33a824

ARG VERSION
ARG COMMIT
ARG JOBS_JOB
LABEL org.opencontainers.image.title="harbor" \
      org.opencontainers.image.source="https://github.com/impulseframework/harbor" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}"

COPY --from=build-env /build /

# The migrate command reads schema (the migrations, the seed) relative to its
# working directory, and the server serves each bundle from the directory its variable
# names, so everything sits under the working directory set below.
COPY --from=web-build-env /src/web/dist/console /home/nonroot/web/dist/console
COPY --from=portal-build-env /src/portal/dist/portal /home/nonroot/portal/dist/portal
COPY schema /home/nonroot/schema

# APP_VERSION is the configuration's version input: the image carries its own release, so
# no deploy step repeats it. PORT is Cloud Run's; the configuration's default, 8080, is what the
# image declares.
ENV APP_VERSION="${VERSION}" \
    APP_JOBS_JOB="${JOBS_JOB}" \
    APP_CONSOLE_DIST=/home/nonroot/web/dist/console \
    APP_PORTAL_DIST=/home/nonroot/portal/dist/portal

WORKDIR /home/nonroot
USER nonroot
EXPOSE 8080

ENTRYPOINT ["/app"]

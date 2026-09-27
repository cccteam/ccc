# bedrock's image: the binary every application's pipeline runs its deploy steps from
# (cloudbuild.yaml runs a step from the image the placement pins by digest, bedrockImage).
# Static, on a distroless base, so it runs in any step and carries nothing else.
#
# Every base image is referenced by digest, never by tag alone. The module replaces
# impulse with the sibling checkout (go.mod), so the build context holds both modules as
# bedrock/ and impulse/: from the ccc checkout's root,
#
#     podman build -t <tools repository>/bedrock:<commit> -f bedrock/Dockerfile .
#     podman push <tools repository>/bedrock:<commit>   # then pin the digest it prints

FROM cgr.dev/chainguard/go@sha256:694c79dc301a249df5f2541aff2d82718d4ef3ff36bfa8a9eaee55ecadd40d16 AS build-env

ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=local

WORKDIR /go/src/ccc

COPY impulse/go.mod impulse/go.sum ./impulse/
COPY bedrock/go.mod bedrock/go.sum ./bedrock/
RUN cd bedrock && go mod download

COPY impulse/ ./impulse/
COPY bedrock/ ./bedrock/

RUN cd bedrock && go build -trimpath -ldflags="-s -w" -o /build/bedrock .

FROM cgr.dev/chainguard/static@sha256:1fe35025bd06652914560fd01af12a705c645c5566a8d40766b2a4769f33a824

LABEL org.opencontainers.image.title="bedrock" \
      org.opencontainers.image.source="https://github.com/cccteam/ccc"

COPY --from=build-env /build/bedrock /bedrock

USER nonroot
ENTRYPOINT ["/bedrock"]

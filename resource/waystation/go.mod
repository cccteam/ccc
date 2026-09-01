module github.com/cccteam/ccc/resource/waystation

go 1.26.6

replace github.com/golang-migrate/migrate/v4 v4.19.1 => github.com/jtwatson/migrate/v4 v4.19.2-beta.0

// waystation is committed in the same repo (and same commits) as the resource
// module it exercises, so it must build against the local source rather than
// a published version that cannot exist yet.
replace github.com/cccteam/ccc/resource => ../

require (
	cloud.google.com/go v0.123.0
	cloud.google.com/go/spanner v1.94.0
	github.com/cccteam/access v0.9.12-0.20260901184320-2cf49b2a0fbb
	github.com/cccteam/ccc v0.3.2
	github.com/cccteam/ccc/accesstypes v0.5.10-0.20260901033722-81ab17f8f363
	github.com/cccteam/ccc/resource v0.10.6
	github.com/cccteam/ccc/tracer v0.1.7
	github.com/cccteam/db-initiator v0.3.13
	github.com/cccteam/httpio v0.7.17
	github.com/cccteam/logger v0.1.27
	github.com/cccteam/session v0.11.1-0.20260826003108-0fa6294c2da7
	github.com/cccteam/spxscan v0.0.14
	github.com/go-chi/chi/v5 v5.3.2
	github.com/go-playground/errors/v5 v5.4.0
	github.com/go-playground/validator/v10 v10.30.3
	github.com/google/go-cmp v0.7.0
	github.com/jtwatson/server v0.2.2
	github.com/jtwatson/spaassets v0.0.0-20160917192555-583a733b0a63
	github.com/sethvargo/go-envconfig v1.4.3
	github.com/shopspring/decimal v1.4.1-0.20260819210736-ca4740823783
	google.golang.org/api v0.293.0
)

require (
	aidanwoods.dev/go-paseto v1.6.0 // indirect
	aidanwoods.dev/go-result v0.3.1 // indirect
	cel.dev/expr v0.25.3 // indirect
	cloud.google.com/go/auth v0.23.1 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.13.0 // indirect
	cloud.google.com/go/logging v1.19.1 // indirect
	cloud.google.com/go/longrunning v1.2.0 // indirect
	cloud.google.com/go/monitoring v1.30.0 // indirect
	cloud.google.com/go/trace v1.16.0 // indirect
	dario.cat/mergo v1.0.2 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/GoogleCloudPlatform/grpc-gcp-go/grpcgcp v1.6.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.35.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace v1.35.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/internal/resourcemapping v0.59.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator v0.59.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/cccteam/ccc/cache v0.1.6 // indirect
	github.com/cccteam/ccc/pkg v0.1.4 // indirect
	github.com/cccteam/ccc/securehash v0.0.13 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudspannerecosystem/memefish v0.8.1 // indirect
	github.com/cncf/xds/go v0.0.0-20260202195803-dba9d589def2 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/platforms v0.2.1 // indirect
	github.com/coreos/go-oidc/v3 v3.20.0 // indirect
	github.com/cpuguy83/dockercfg v0.3.2 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-connections v0.8.1 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/ebitengine/purego v0.10.2 // indirect
	github.com/envoyproxy/go-control-plane/envoy v1.39.0 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.3.3 // indirect
	github.com/ettle/strcase v0.2.0 // indirect
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.2 // indirect
	github.com/gabriel-vasile/mimetype v1.4.13 // indirect
	github.com/georgysavva/scany/v2 v2.1.4 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/pkg/v5 v5.31.0 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/gofrs/uuid v4.4.0+incompatible // indirect
	github.com/golang-migrate/migrate/v4 v4.19.1 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.21 // indirect
	github.com/googleapis/gax-go/v2 v2.23.0 // indirect
	github.com/jackc/pgerrcode v0.0.0-20250907135507-afb5586c32a6 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx-shopspring-decimal v0.0.0-20220624020537-1d36b5a1853e // indirect
	github.com/jackc/pgx/v5 v5.10.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jtwatson/shutdown v0.1.1 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/lib/pq v1.12.3 // indirect
	github.com/lufia/plan9stats v0.0.0-20260802145828-341c2f0c90b5 // indirect
	github.com/magiconair/properties v1.18.11 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.3.3 // indirect
	github.com/moby/moby/api v1.55.0 // indirect
	github.com/moby/moby/client v0.5.1 // indirect
	github.com/moby/patternmatcher v0.6.1 // indirect
	github.com/moby/sys/sequential v0.7.0 // indirect
	github.com/moby/sys/user v0.4.1 // indirect
	github.com/moby/sys/userns v0.2.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/momaek/formattag v0.0.10 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/power-devops/perfstat v0.0.0-20260805114148-88456608a4f6 // indirect
	github.com/shirou/gopsutil/v4 v4.26.7 // indirect
	github.com/sirupsen/logrus v1.10.0 // indirect
	github.com/smartystreets/goconvey v1.8.1 // indirect
	github.com/spiffe/go-spiffe/v2 v2.8.1 // indirect
	github.com/stretchr/testify v1.12.0 // indirect
	github.com/testcontainers/testcontainers-go v0.44.0 // indirect
	github.com/tklauser/go-sysconf v0.4.0 // indirect
	github.com/tklauser/numcpus v0.12.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/detectors/gcp v1.45.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.70.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.70.0 // indirect
	go.opentelemetry.io/otel v1.45.0 // indirect
	go.opentelemetry.io/otel/metric v1.45.0 // indirect
	go.opentelemetry.io/otel/sdk v1.45.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.45.0 // indirect
	go.opentelemetry.io/otel/trace v1.45.0 // indirect
	go.uber.org/mock v0.6.0 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/mod v0.39.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/genproto v0.0.0-20260818201246-1b0934165a6f // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260818201246-1b0934165a6f // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260818201246-1b0934165a6f // indirect
	google.golang.org/grpc v1.83.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

tool go.uber.org/mock/mockgen
